"""Resident Nano/Turbo adapter. stdout is JSON protocol; model diagnostics go to stderr."""
import sys
protocol = sys.stdout
sys.stdout = sys.stderr
import argparse
import base64
import io
import json
import os
import queue
import threading
import wave

os.environ.update(OMP_NUM_THREADS="2", MKL_NUM_THREADS="2", HF_HUB_DISABLE_TELEMETRY="1")
parser = argparse.ArgumentParser()
parser.add_argument("--model", required=True)
parser.add_argument("--variant",choices=("nano","turbo"),default="nano")
args = parser.parse_args()
os.environ["HF_HOME"] = os.path.join(os.path.dirname(args.model), "hub")
import torch
from chatterbox.tts_turbo import ChatterboxTurboTTS
torch.set_num_threads(2)
torch.set_num_interop_threads(1)
if not torch.cuda.is_available():
    raise RuntimeError("Chatterbox profile requires the CUDA Python environment")
model = ChatterboxTurboTTS.from_local(args.model, "cuda", nano=args.variant == "nano")
pending = queue.Queue(maxsize=1)
jobs = {}
lock = threading.Lock()

def reply(**values):
    protocol.write(json.dumps(values, separators=(",", ":")) + "\n")
    protocol.flush()

class Interrupted(Exception):
    pass

current = None
def checkpoint(module, inputs):
    if current is not None and current.is_set():
        raise Interrupted()

model.t3.tfmr.register_forward_pre_hook(checkpoint)
for module in model.s3gen.modules():
    if not list(module.children()):
        module.register_forward_pre_hook(checkpoint)

def read_requests():
    try:
        while True:
            line = sys.stdin.readline(65537)
            if not line:
                break
            if len(line) > 65536 or not line.endswith("\n"):
                raise ValueError("oversized request")
            request = json.loads(line)
            ident = request["id"]
            if request["op"] == "cancel":
                with lock:
                    event = jobs.get(ident)
                    if event is not None:
                        event.set()
            elif request["op"] == "synthesize":
                text = request["text"]
                if not isinstance(text, str) or not text.strip() or len(text.encode()) > 8192:
                    raise ValueError("invalid phrase")
                event = threading.Event()
                with lock:
                    if jobs:
                        raise ValueError("worker accepts one active request")
                    jobs[ident] = event
                pending.put_nowait((ident, text, event))
            else:
                raise ValueError("unknown operation")
    except Exception as error:
        print(f"Protocol error: {error}", file=sys.stderr, flush=True)
    finally:
        # Parent closure always ends the CUDA owner, even during inference.
        os._exit(0)

reply(type="ready")
threading.Thread(target=read_requests, daemon=True).start()
while True:
    ident, text, current = pending.get()
    result = {"id": ident}
    try:
        if current.is_set():
            raise Interrupted()
        with torch.inference_mode():
            audio = model.generate(text)
        if current.is_set():
            raise Interrupted()
        samples = audio.squeeze().cpu().numpy()
        import numpy as np
        if not np.isfinite(samples).all() or not 0 < len(samples) <= model.sr * 60:
            raise ValueError("invalid generated audio")
        output = io.BytesIO()
        with wave.open(output, "wb") as wav:
            wav.setnchannels(1)
            wav.setsampwidth(2)
            wav.setframerate(model.sr)
            wav.writeframes((np.clip(samples, -1, 1) * 32767).astype("<i2").tobytes())
        if current.is_set():
            raise Interrupted()
        result.update(status="ok", wav=base64.b64encode(output.getvalue()).decode())
    except Interrupted:
        result.update(status="cancelled")
    except Exception as error:
        result.update(status="error", error=str(error))
    finally:
        with lock:
            jobs.pop(ident, None)
        current = None
    reply(**result)
