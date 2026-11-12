package lucide

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The package holds every Lucide icon, which is only affordable while each
// variable is statically initialized: the linker then drops the ones a
// program does not reference. Package init code would keep all of them.
func TestLinkerDropsUnusedIcons(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	exe := filepath.Join(t.TempDir(), "linkcheck.exe")
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/linkcheck").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "tool", "nm", exe).CombinedOutput()
	if err != nil {
		t.Fatalf("go tool nm: %v\n%s", err, out)
	}

	const prefix = "github.com/myparsleycat/fairy/icons/lucide."
	var linked []string
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if name, ok := strings.CutPrefix(fields[len(fields)-1], prefix); ok {
			linked = append(linked, name)
		}
	}
	slices.Sort(linked)
	if want := []string{"Folder", "Loader2"}; !slices.Equal(linked, want) {
		t.Errorf("linked lucide symbols = %v, want %v", linked, want)
	}
}
