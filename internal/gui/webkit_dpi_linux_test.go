//go:build linux && cgo && gtk3 && !nogui

package gui

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// GTK and WebKit cache process-wide settings. Each case uses a native child
// with an isolated home and the same compatibility helper as the desktop app.
func TestGTK3WebkitDPI(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("native GTK3/WebKit test needs a display")
	}
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gtk+-3.0", "webkit2gtk-4.1").Output()
	if err != nil {
		t.Fatalf("native probe dependencies: %v", err)
	}
	dir := t.TempDir()
	source, err := filepath.Abs("testdata/gtk3_dpi_probe.c")
	if err != nil {
		t.Fatal(err)
	}
	include, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{source, "-DMAGPIE_DPI_GUARD", "-I" + include, "-o", bin}, strings.Fields(string(flags))...)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("build native probe: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		name       string
		gdk, gtk   int
		scale      string
		device     string
		zoom, want float64
	}{
		{"unset", -1, -1, "1", "1", 1, 96},
		{"zero", 0, -1, "1", "1", 1, 96},
		{"configured", -1, 144 * 1024, "1", "1", 1, 144},
		{"existing", 120, 144 * 1024, "1.25", "1", 1, 120},
		{"font-scale", -1, -1, "1.25", "1", 1, 120},
		{"configured-scale", -1, 144 * 1024, "1.25", "1", 1, 180},
		{"device-scale", -1, -1, "1", "2", 1, 96},
		{"text-zoom", -1, -1, "1", "1", 1.1, 96},
		{"invalid-scale", -1, -1, "garbage", "1", 1, 96},
		{"negative-scale", -1, -1, "-1", "1", 1, 96},
		{"zero-scale", -1, -1, "0", "1", 1, 96},
		{"nonfinite-scale", -1, -1, "nan", "1", 1, 96},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, ".config", "gtk-3.0")
			if err := os.MkdirAll(config, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config, "settings.ini"), []byte(fmt.Sprintf("[Settings]\ngtk-xft-dpi=%d\n", tc.gtk)), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, strconv.Itoa(tc.gdk), fmt.Sprint(tc.zoom))
			cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"), "XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "GSETTINGS_BACKEND=memory", "GDK_DPI_SCALE="+tc.scale, "GDK_SCALE="+tc.device, "WEBKIT_DISABLE_COMPOSITING_MODE=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("native WebKit probe: %v\n%s", err, out)
			}
			seen := 0
			for _, line := range strings.Split(string(out), "\n") {
				var id, allocated, device, initialDevice int
				var dpi, width, height, content, font float64
				if n, _ := fmt.Sscanf(line, "layout %d %f %f %f %f %f %d %d %d", &id, &dpi, &width, &height, &content, &font, &allocated, &device, &initialDevice); n != 9 {
					continue
				}
				seen++
				t.Log(line)
				for _, v := range []float64{width, height, content, font} {
					if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
						t.Fatalf("unusable native layout: %s", line)
					}
				}
				if dpi != tc.want || device != initialDevice {
					t.Errorf("DPI/device scale changed: %s; want DPI %g and initial device scale", line, tc.want)
				}
				wantWidth := float64(allocated) * 96 / tc.want / tc.zoom
				if math.Abs(width-wantWidth) > 2 || content < width/2 || math.Abs(font-16) > 0.1 {
					t.Errorf("incorrect content width or scale: %s; want viewport %g", line, wantWidth)
				}
			}
			if seen != 2 {
				t.Fatalf("expected main and panel layout measurements, got %d\n%s", seen, out)
			}
		})
	}
	t.Run("no-display", func(t *testing.T) {
		cmd := exec.Command(bin, "-1", "1")
		cmd.Env = append(os.Environ(), "DISPLAY=", "WAYLAND_DISPLAY=", "GDK_BACKEND=wayland")
		out, err := cmd.CombinedOutput()
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 3 {
			t.Fatalf("expected GTK initialization failure (exit 3), got %v\n%s", err, out)
		}
	})
}
