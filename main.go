package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

//go:embed assets/ffmpeg.exe
var embedded embed.FS

type Settings struct {
	RecordingFolder string `json:"recording_folder"`
	OutputFolder    string `json:"output_folder"`
	ClipSeconds     int    `json:"clip_seconds"`
}

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true,
	".avi": true, ".webm": true, ".flv": true,
}

func main() {
	a := app.NewWithID("gg.wobblucy.quickclip")
	w := a.NewWindow("QuickClip")
	w.Resize(fyne.NewSize(560, 320))

	settings := loadSettings()
	ffmpegPath, err := ensureFfmpeg()
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not unpack ffmpeg: %w", err), w)
	}

	folderLabel := widget.NewLabel("")
	latestLabel := widget.NewLabel("")
	latestLabel.Wrapping = fyne.TextWrapWord
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	refresh := func() {
		if settings.RecordingFolder == "" {
			folderLabel.SetText("Recording folder: (not set)")
			latestLabel.SetText("Pick a folder to get started.")
			return
		}
		folderLabel.SetText("Recording folder: " + settings.RecordingFolder)
		latest, err := findLatestVideo(settings.RecordingFolder)
		if err != nil {
			latestLabel.SetText("Could not read folder: " + err.Error())
			return
		}
		if latest == "" {
			latestLabel.SetText("No videos found in that folder yet.")
			return
		}
		info, _ := os.Stat(latest)
		latestLabel.SetText(fmt.Sprintf("Most recent: %s\n(%s) - click Clip to pick this or browse for another",
			filepath.Base(latest),
			info.ModTime().Format("Mon Jan 2, 3:04 PM")))
	}

	pickFolderBtn := widget.NewButton("Change recording folder", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			settings.RecordingFolder = uri.Path()
			saveSettings(settings)
			refresh()
		}, w)
	})

	clipBtn := widget.NewButton("Clip", func() {
		if settings.RecordingFolder == "" {
			dialog.ShowInformation("Pick a folder first", "Use 'Change recording folder' to choose where your videos live.", w)
			return
		}
		showRecentPicker(a, w, settings, ffmpegPath, status)
	})
	clipBtn.Importance = widget.HighImportance

	settingsBtn := widget.NewButton("Settings", func() {
		showSettings(a, settings)
	})

	w.SetContent(container.NewVBox(
		widget.NewLabelWithStyle("QuickClip", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		folderLabel,
		latestLabel,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, clipBtn, pickFolderBtn),
		settingsBtn,
		widget.NewSeparator(),
		status,
	))

	refresh()

	if settings.RecordingFolder == "" {
		go func() {
			time.Sleep(200 * time.Millisecond)
			dialog.ShowInformation("Welcome", "Pick the folder where your recordings are saved. QuickClip remembers it.", w)
		}()
	}

	w.ShowAndRun()
}

func showRecentPicker(a fyne.App, parent fyne.Window, s *Settings, ffmpegPath string, status *widget.Label) {
	videos := listVideos(s.RecordingFolder, 200)

	pw := a.NewWindow("Pick a video to clip")
	pw.Resize(fyne.NewSize(640, 440))

	if len(videos) == 0 {
		pw.SetContent(container.NewVBox(
			widget.NewLabel("No videos found in "+s.RecordingFolder),
			widget.NewButton("Browse...", func() {
				openSystemPicker(parent, s, ffmpegPath, status)
				pw.Close()
			}),
		))
		pw.Show()
		return
	}

	selectedIdx := 0
	list := widget.NewList(
		func() int { return len(videos) },
		func() fyne.CanvasObject {
			return container.NewVBox(
				widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewLabel(""),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			v := videos[i]
			box.Objects[0].(*widget.Label).SetText(filepath.Base(v.path))
			box.Objects[1].(*widget.Label).SetText(humanAgo(v.mtime))
		},
	)
	list.OnSelected = func(i widget.ListItemID) { selectedIdx = i }
	list.Select(0)

	clipBtn := widget.NewButton("Clip selected", func() {
		chosen := videos[selectedIdx].path
		pw.Close()
		askTimestamp(parent, chosen, ffmpegPath, s, status)
	})
	clipBtn.Importance = widget.HighImportance

	browseBtn := widget.NewButton("Browse for another...", func() {
		pw.Close()
		openSystemPicker(parent, s, ffmpegPath, status)
	})

	cancelBtn := widget.NewButton("Cancel", func() { pw.Close() })

	pw.SetContent(container.NewBorder(
		widget.NewLabel("Most recent first. Pick one, then click Clip selected."),
		container.NewGridWithColumns(3, clipBtn, browseBtn, cancelBtn),
		nil, nil,
		list,
	))
	pw.Show()
}

func openSystemPicker(w fyne.Window, s *Settings, ffmpegPath string, status *widget.Label) {
	fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		chosen := rc.URI().Path()
		rc.Close()
		askTimestamp(w, chosen, ffmpegPath, s, status)
	}, w)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".mp4", ".mkv", ".mov", ".avi", ".webm", ".flv"}))
	if startURI, err := storage.ListerForURI(storage.NewFileURI(s.RecordingFolder)); err == nil {
		fd.SetLocation(startURI)
	}
	fd.Resize(fyne.NewSize(800, 560))
	fd.Show()
}

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d day(s) ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2, 3:04 PM")
	}
}

func askTimestamp(w fyne.Window, video, ffmpegPath string, s *Settings, status *widget.Label) {
	tsEntry := widget.NewEntry()
	tsEntry.SetPlaceHolder("e.g. 1:23 or 83 or 0:01:23")

	durEntry := widget.NewEntry()
	durEntry.SetText(strconv.Itoa(s.ClipSeconds))

	form := dialog.NewForm("Clip "+filepath.Base(video), "Clip", "Cancel",
		[]*widget.FormItem{
			{Text: "Start", Widget: tsEntry},
			{Text: "Length (s)", Widget: durEntry},
		},
		func(ok bool) {
			if !ok {
				return
			}
			start, err := parseTimestamp(tsEntry.Text)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			dur, err := strconv.Atoi(strings.TrimSpace(durEntry.Text))
			if err != nil || dur <= 0 {
				dialog.ShowError(fmt.Errorf("length must be a positive whole number of seconds"), w)
				return
			}
			s.ClipSeconds = dur
			saveSettings(s)

			status.SetText("Clipping...")
			go func() {
				out, err := makeClip(ffmpegPath, video, start, dur, s.OutputFolder)
				if err != nil {
					status.SetText("")
					dialog.ShowError(err, w)
					return
				}
				status.SetText("Saved: " + filepath.Base(out))
				showClipDone(w, out, s)
			}()
		}, w)
	form.Resize(fyne.NewSize(420, 180))
	form.Show()
}

func showClipDone(w fyne.Window, clipPath string, _ *Settings) {
	openBtn := widget.NewButton("Open folder", func() {
		exec.Command("explorer", "/select,", clipPath).Start()
	})
	openBtn.Importance = widget.HighImportance

	content := container.NewVBox(
		widget.NewLabel("Clip saved:"),
		widget.NewLabel(filepath.Base(clipPath)),
		openBtn,
	)
	d := dialog.NewCustom("Done", "Close", content, w)
	d.Resize(fyne.NewSize(420, 180))
	d.Show()
}

func showSettings(a fyne.App, s *Settings) {
	w := a.NewWindow("QuickClip Settings")
	w.Resize(fyne.NewSize(540, 260))

	outputEntry := widget.NewEntry()
	outputEntry.SetText(s.OutputFolder)

	pickOutputBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			outputEntry.SetText(uri.Path())
		}, w)
	})

	saveBtn := widget.NewButton("Save", func() {
		s.OutputFolder = strings.TrimSpace(outputEntry.Text)
		saveSettings(s)
		w.Close()
	})
	saveBtn.Importance = widget.HighImportance

	w.SetContent(container.NewVBox(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Output folder (where clips are saved):"),
		container.NewBorder(nil, nil, nil, pickOutputBtn, outputEntry),
		widget.NewSeparator(),
		saveBtn,
	))
	w.Show()
}

func parseTimestamp(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("enter a timestamp like 1:23 or 83")
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		v, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("not a number: %s", parts[0])
		}
		return v, nil
	case 2:
		m, err1 := strconv.ParseFloat(parts[0], 64)
		sec, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("bad MM:SS format")
		}
		return m*60 + sec, nil
	case 3:
		h, err1 := strconv.ParseFloat(parts[0], 64)
		m, err2 := strconv.ParseFloat(parts[1], 64)
		sec, err3 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, fmt.Errorf("bad HH:MM:SS format")
		}
		return h*3600 + m*60 + sec, nil
	}
	return 0, fmt.Errorf("use SS, MM:SS, or HH:MM:SS")
}

type videoEntry struct {
	path  string
	mtime time.Time
}

func listVideos(root string, limit int) []videoEntry {
	var out []videoEntry
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !videoExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, videoEntry{path: path, mtime: info.ModTime()})
		return nil
	})

	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func findLatestVideo(root string) (string, error) {
	var files []os.DirEntry
	walk := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				sub := filepath.Join(dir, e.Name())
				inner, err := os.ReadDir(sub)
				if err == nil {
					for _, ie := range inner {
						if !ie.IsDir() && videoExts[strings.ToLower(filepath.Ext(ie.Name()))] {
							files = append(files, fakeDirEntry{full: filepath.Join(sub, ie.Name()), e: ie})
						}
					}
				}
				continue
			}
			if videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
				files = append(files, fakeDirEntry{full: filepath.Join(dir, e.Name()), e: e})
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Slice(files, func(i, j int) bool {
		ii, _ := files[i].Info()
		jj, _ := files[j].Info()
		return ii.ModTime().After(jj.ModTime())
	})
	return files[0].(fakeDirEntry).full, nil
}

type fakeDirEntry struct {
	full string
	e    os.DirEntry
}

func (f fakeDirEntry) Name() string               { return f.e.Name() }
func (f fakeDirEntry) IsDir() bool                { return f.e.IsDir() }
func (f fakeDirEntry) Type() os.FileMode          { return f.e.Type() }
func (f fakeDirEntry) Info() (os.FileInfo, error) { return f.e.Info() }

func makeClip(ffmpegPath, source string, start float64, duration int, outputDir string) (string, error) {
	if outputDir == "" {
		outputDir = defaultOutputDir()
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	tag := strings.ReplaceAll(fmt.Sprintf("%.2f", start), ".", "p")
	outFile := filepath.Join(outputDir, fmt.Sprintf("%s_t%s_%ds.mp4", stem, tag, duration))

	encoder := pickEncoder(ffmpegPath)

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%f", start),
		"-i", source,
		"-t", strconv.Itoa(duration),
		"-vf", "scale=-2:720",
		"-r", "60",
	}
	args = append(args, encoder...)
	args = append(args,
		"-an",
		"-movflags", "+faststart",
		"-y", outFile,
	)

	cmd := exec.Command(ffmpegPath, args...)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed: %v\n%s", err, stderr.String())
	}
	return outFile, nil
}

func pickEncoder(ffmpegPath string) []string {
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-encoders")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err == nil && bytes.Contains(out, []byte("h264_nvenc")) {
		return []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-cq", "21", "-b:v", "4M", "-maxrate", "6M"}
	}
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "20"}
}

func ensureFfmpeg() (string, error) {
	dir := filepath.Join(configDir(), "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "ffmpeg.exe")

	src, err := embedded.Open("assets/ffmpeg.exe")
	if err != nil {
		return "", err
	}
	defer src.Close()
	srcInfo, _ := embedded.ReadFile("assets/ffmpeg.exe")

	if existing, err := os.Stat(dest); err == nil && existing.Size() == int64(len(srcInfo)) {
		return dest, nil
	}

	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return "", err
	}
	out.Close()
	return dest, nil
}

func configDir() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		d, _ = os.UserHomeDir()
	}
	return filepath.Join(d, "QuickClip")
}

func settingsPath() string {
	return filepath.Join(configDir(), "settings.json")
}

func defaultOutputDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Videos", "QuickClip")
}

func loadSettings() *Settings {
	s := &Settings{ClipSeconds: 10}
	data, err := os.ReadFile(settingsPath())
	if err == nil {
		_ = json.Unmarshal(data, s)
	}
	if s.ClipSeconds <= 0 {
		s.ClipSeconds = 10
	}
	if s.OutputFolder == "" {
		s.OutputFolder = defaultOutputDir()
	}
	return s
}

func saveSettings(s *Settings) {
	_ = os.MkdirAll(configDir(), 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(settingsPath(), data, 0644)
}

