# QuickClip

Tiny Windows app for snipping short clips out of your gameplay recordings. Made for sharing the play of the night without firing up a video editor.

Point it at your recording folder, hit **Clip**, type a timestamp, get an MP4. Drag it straight into Discord (or wherever) from the output folder.

## Install

1. Grab `QuickClip.exe` from the [Releases page](https://github.com/Wobblucy/quickclip/releases).
2. Drop it anywhere. Double-click.

That's it. ffmpeg is built in, no PATH setup, no installer.

## How to use

- **First run** asks for the folder your recorder saves videos into. It remembers this.
- The main window always shows the most recently modified video in that folder.
- Hit **Clip**. Type a timestamp. Pick a length. Done.
- Output lands in `Videos\QuickClip\` by default. Change it under Settings.

### Timestamp formats

```
83          83 seconds in
1:23        1 minute 23 seconds
0:01:23     same thing, with hours
```

## What it does under the hood

```
ffmpeg -ss <start> -i <recording> -t <len> -vf scale=-2:720 -r 60 \
       -c:v h264_nvenc -an -movflags +faststart out.mp4
```

(Audio is dropped to keep clip files small. `-an` means "no audio".)

That's the entire feature. Uses NVENC if your GPU has it, falls back to libx264 otherwise.

## Build from source

You need Go 1.22+ and a mingw-w64 gcc (because Fyne uses CGO).

```powershell
.\scripts\fetch-ffmpeg.ps1
go build -ldflags "-H windowsgui -s -w" -o QuickClip.exe .
```

## Why this exists

I wanted to share a clip from a raid pull in under 10 seconds without opening an editor. Existing tools all wanted me to install something, sign up for something, or upload first. This is a `Click → Type → Done` shape and it stays out of your way.

Pull requests welcome.
