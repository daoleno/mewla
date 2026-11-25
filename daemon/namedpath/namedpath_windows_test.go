package namedpath

import (
	"path/filepath"
	"testing"
)

// Windows paths as agents name them (owner's Windows user 11-24: Brain named
// `D:\Project\AN\01_策略研究\…\explainer_20261124\` and nothing opened): backslashes, a drive, CJK names, `~\`,
// and a folder named with its closing backslash.
func TestWindowsNamedPathsOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	writeFiles(t, cwd, "reports/q3.html", "site/index.html", "01_策略研究/outputs/explainer_20261124/黄金策略图解.html", "01_策略研究/outputs/explainer_20261124/index.html")
	writeFiles(t, home, "mewla-path-test/home.html", "pages/index.html")
	explainer := filepath.Join(cwd, "01_策略研究", "outputs", "explainer_20261124")
	for reference, want := range map[string]string{
		explainer + `\黄金策略图解.html`:                   filepath.Join(explainer, "黄金策略图解.html"),
		filepath.ToSlash(explainer) + "/黄金策略图解.html": filepath.Join(explainer, "黄金策略图解.html"),
		explainer + `\`:               filepath.Join(explainer, "index.html"),
		`.\reports\q3.html`:           filepath.Join(cwd, "reports", "q3.html"),
		`reports\q3.html`:             filepath.Join(cwd, "reports", "q3.html"),
		`site\`:                       filepath.Join(cwd, "site", "index.html"),
		`.\site\`:                     filepath.Join(cwd, "site", "index.html"),
		`~\mewla-path-test\home.html`: filepath.Join(home, "mewla-path-test", "home.html"),
		`~\pages\`:                    filepath.Join(home, "pages", "index.html"),
	} {
		if got := opens(cwd, reference); got != canonical(t, want) {
			t.Errorf("%q opens %q, want %q", reference, got, want)
		}
	}
	for _, reference := range []string{`site`, `reports\`, `~\missing\`} {
		if got := opens(cwd, reference); got != "" {
			t.Errorf("%q opens %q, want nothing", reference, got)
		}
	}
}
