param([Parameter(Mandatory=$true)][string]$SourceAssets)
$ErrorActionPreference='Stop'
$source=[IO.Path]::GetFullPath($SourceAssets).TrimEnd('\','/')
$target=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../assets')).TrimEnd('\','/')
if ($source -eq $target) {throw 'Source and destination must differ'}
Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices;public static class VoiceAssetLinks { [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] public static extern bool CreateHardLink(string target,string source,IntPtr unused); }'
function Hash([string]$path){$stream=[IO.File]::OpenRead($path);$sha=[Security.Cryptography.SHA256]::Create();try{return ([BitConverter]::ToString($sha.ComputeHash($stream))).Replace('-','').ToLowerInvariant()}finally{$stream.Dispose();$sha.Dispose()}}
$manifest=Get-Content -LiteralPath (Join-Path $source 'chatterbox-manifest.json') -Raw | ConvertFrom-Json
$entries=@($manifest.files)
foreach($directory in @('runtimes/whisper-cuda/Release','runtimes/llamacpp','runtimes/sherpa/lib')) {
 foreach($file in Get-ChildItem -LiteralPath (Join-Path $source $directory) -File -Recurse){$entries+=@{path=$file.FullName.Substring($source.Length+1).Replace('\','/');size=$file.Length;sha256=(Hash $file.FullName)}}
}
foreach($name in @('models/whisper/ggml-small-q5_1.bin','models/qwen/Qwen3-1.7B-Q8_0.gguf','models/vad/silero_vad.onnx')){$path=Join-Path $source $name;$entries+=@{path=$name;size=(Get-Item -LiteralPath $path).Length;sha256=(Hash $path)}}
$vadWorker=[IO.Path]::GetFullPath((Join-Path $source '../bin/vad-worker.exe'))
$sourceProject=[IO.Path]::GetDirectoryName($source)
if(!$vadWorker.StartsWith($sourceProject+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'VAD worker source escapes source project'}
$entries+=@{path='runtimes/silero/vad-worker.exe';source_file=$vadWorker;size=(Get-Item -LiteralPath $vadWorker).Length;sha256=(Hash $vadWorker)}
$count=0
$installedEntries=@()
foreach($entry in $entries){
 $destinationPath=$entry.path -replace '^models/chatterbox/', 'models/speaking/chatterbox/' -replace '^models/qwen/', 'models/thinking/qwen/' -replace '^models/whisper/', 'models/listening/whisper/' -replace '^models/vad/', 'models/listening/silero/'
 $src=[IO.Path]::GetFullPath((Join-Path $source $entry.path));$dst=[IO.Path]::GetFullPath((Join-Path $target $destinationPath))
 $installedEntries+=@{path=$destinationPath;source_path=$entry.path;size=$entry.size;sha256=$entry.sha256}
 if($entry.source_file){$src=$entry.source_file}
 if(!$entry.source_file -and !$src.StartsWith($source+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase) -or !$dst.StartsWith($target+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Asset manifest path escapes root'}
 if((Get-Item -LiteralPath $src).Length -ne $entry.size -or (Hash $src) -ne $entry.sha256){throw "Source checksum mismatch: $($entry.path)"}
 [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($dst)) | Out-Null
 if(Test-Path -LiteralPath $dst){if((Get-Item -LiteralPath $dst).Length -ne $entry.size -or (Hash $dst) -ne $entry.sha256){throw "Existing destination differs: $($entry.path)"}}
 elseif(![VoiceAssetLinks]::CreateHardLink($dst,$src,[IntPtr]::Zero)){Copy-Item -LiteralPath $src -Destination $dst}
 $count++;if($count%5000 -eq 0){Write-Host "$count / $($entries.Count) verified assets"}
}
[ordered]@{version=1;platform='windows-x86_64';source='imported verified local runtime and model bundle';files=$installedEntries} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $target 'manifest.json') -Encoding utf8
Write-Host "Imported $count assets. Same-volume files use hardlinks; no source files were moved."
