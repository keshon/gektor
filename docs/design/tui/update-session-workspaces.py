"""Update the existing session boards. Run once against the pre-workspace library.
Writes command JSON; apply with figcraft-cli run ... --in ... --out ... .
The numeric IDs belong to the checked-in GEKTOR TUI library.
"""
import json
from pathlib import Path

commands = []
def set_node(node, **props):
    commands.append({"command": "node.set", "params": {"id": node, **props}})

for frame, offset, fg, muted, selected in [
    (39, 0, "#D5DEE7", "#9DABB7", "#F1F4F8"),
    (40, 193, "#253542", "#596A79", "#FFFFFF"),
]:
    set_node(238 + offset, characters="Open session  /  All workspaces")
    set_node(252 + offset, y=160, height=60)
    for node, y, text in [
        (253, 160, "› Review browser observations"),
        (254, 200, "  Today 13:57     26 messages"),
        (255, 240, "  Prepare image workflow"),
        (256, 280, "  Today 13:03     85 messages"),
        (257, 320, "  Repository report"),
        (258, 360, "  Yesterday       42 messages"),
    ]:
        set_node(node + offset, y=y, characters=text)
    # The content region gains one row; footer remains within the 80x24 frame.
    set_node(241 + offset, y=390)
    set_node(242 + offset, x=40, y=400, width=700,
             characters="Enter Open   ^F Scope   ^W Assign   Esc Close")
    for node in range(243, 250):
        set_node(node + offset, visible=False)
    set_node(259 + offset, y=100, characters="3 sessions  ·  ^F: All / Current workspace")
    for y, text, color in [
        (180, "  D:\\Projects\\dev\\keshon\\gektor", selected),
        (260, "  D:\\Projects\\image-tools", muted),
        (340, "  Unassigned workspace", muted),
    ]:
        commands.append({"command": "text.create", "params": {
            "parent": frame, "name": "Session workspace", "text": text,
            "x": 50, "y": y, "width": 682,
            "props": {"height": 20, "fontFamily": "Liberation Mono", "fontSize": 16.66,
                      "lineHeight": 20, "fill": color, "textAutoResize": "none"}
        }})

Path(__file__).with_suffix('.json').write_text(json.dumps(commands, indent=2), encoding='utf-8')
