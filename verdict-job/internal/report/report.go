// Package report renders JSON + self-contained HTML reports from an eval.Result.
package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

//go:embed template.html.tmpl
var htmlTmpl string

type View struct {
	*eval.Result
	ScenarioName    string
	LoadDurationSec int
	GrafanaURL      string
	ArgoURL         string
	JSONURL         string
}

var funcs = template.FuncMap{
	"deref": func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	},
}

func RenderJSON(w io.Writer, v View) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func RenderHTML(w io.Writer, v View) error {
	t, err := template.New("r").Funcs(funcs).Parse(htmlTmpl)
	if err != nil {
		return err
	}
	return t.Execute(w, v)
}

// Write writes both report.json and report.html under dir.
func Write(dir string, v View) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	jpath := filepath.Join(dir, "report.json")
	hpath := filepath.Join(dir, "report.html")
	jf, err := os.Create(jpath)
	if err != nil {
		return "", "", err
	}
	defer jf.Close()
	if err := RenderJSON(jf, v); err != nil {
		return "", "", err
	}
	hf, err := os.Create(hpath)
	if err != nil {
		return "", "", err
	}
	defer hf.Close()
	if err := RenderHTML(hf, v); err != nil {
		return "", "", err
	}
	return jpath, hpath, nil
}

func MustOpen(p string) *os.File {
	f, err := os.Open(p)
	if err != nil {
		panic(fmt.Errorf("open %s: %w", p, err))
	}
	return f
}
