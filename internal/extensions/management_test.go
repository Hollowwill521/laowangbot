package extensions

import (
	"context"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"strings"
	"testing"
)

type memoryTPM struct {
	installed []plugin.InstalledInfo
	catalog   []plugin.CatalogEntry
	calls     []string
	fail      map[string]error
}

func (m *memoryTPM) List() ([]plugin.Manifest, error)           { return nil, nil }
func (m *memoryTPM) Installed() ([]plugin.InstalledInfo, error) { return m.installed, nil }
func (m *memoryTPM) Search(_ context.Context, q string) ([]plugin.CatalogEntry, error) {
	return m.catalog, nil
}
func (m *memoryTPM) InstallRemote(_ context.Context, n string) error {
	m.calls = append(m.calls, "install:"+n)
	return m.fail[n]
}
func (m *memoryTPM) UpdateRemoteForce(_ context.Context, n string, f bool) error {
	v := "update:"
	if f {
		v = "force:"
	}
	m.calls = append(m.calls, v+n)
	return m.fail[n]
}
func (m *memoryTPM) Remove(n string) error       { m.calls = append(m.calls, "remove:"+n); return m.fail[n] }
func (m *memoryTPM) InstallLocal(n string) error { m.calls = append(m.calls, "local:"+n); return nil }
func (m *memoryTPM) ReplaceLocal(n string) error { m.calls = append(m.calls, "replace:"+n); return nil }
func (m *memoryTPM) Export(n string) ([]byte, error) {
	m.calls = append(m.calls, "export:"+n)
	return []byte("zip"), nil
}
func (m *memoryTPM) ImportPackage(b []byte, f bool) (string, error) {
	m.calls = append(m.calls, "package")
	return "demo", nil
}
func TestTPMBatchDeduplicatesAndContinuesFailures(t *testing.T) {
	m := &memoryTPM{fail: map[string]error{"bad": errors.New("unavailable")}}
	r, e := executeTPM(context.Background(), m, []string{"i", "ok", "bad", "ok", "last"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(m.calls, ",") != "install:ok,install:bad,install:last" {
		t.Fatal(m.calls)
	}
	if !strings.Contains(r.Text, "成功 2") || !strings.Contains(r.Text, "失败 1") {
		t.Fatal(r.Text)
	}
}
func TestTPMUpdateAllSkipsManualEvenForced(t *testing.T) {
	m := &memoryTPM{installed: []plugin.InstalledInfo{{Manifest: plugin.Manifest{Name: "remote"}, Source: plugin.CatalogURL}, {Manifest: plugin.Manifest{Name: "local"}, Source: "manual"}}}
	r, e := executeTPM(context.Background(), m, []string{"ua", "-f"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(m.calls, ",") != "force:remote" || !strings.Contains(r.Text, "跳过 1") {
		t.Fatal(m.calls, r.Text)
	}
}
func TestTPMReportsModifiedAsSkipped(t *testing.T) {
	m := &memoryTPM{fail: map[string]error{"changed": plugin.ErrModified}}
	r, e := executeTPM(context.Background(), m, []string{"update", "changed"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Text, "跳过 1") || !strings.Contains(r.Text, "-f") {
		t.Fatal(r.Text)
	}
}
func TestTPMRejectsInvalidArgsBeforeMutation(t *testing.T) {
	for _, args := range [][]string{{"rm"}, {"i", "all", "demo"}, {"update", "--unknown"}, {"upload", "one", "two"}, {"source", "add", "url"}} {
		m := &memoryTPM{}
		if _, e := executeTPM(context.Background(), m, args, nil); e == nil {
			t.Errorf("accepted %v", args)
		}
		if len(m.calls) > 0 {
			t.Fatal(m.calls)
		}
	}
}
func TestTPMInstallWithoutNameRequestsPackage(t *testing.T) {
	r, e := executeTPM(context.Background(), &memoryTPM{}, []string{"install"}, nil)
	if e != nil || !r.ImportReply {
		t.Fatal(r, e)
	}
}
func TestTPMRemoveAllAndVerbose(t *testing.T) {
	m := &memoryTPM{installed: []plugin.InstalledInfo{{Manifest: plugin.Manifest{Name: "demo", Version: "1", Commands: []string{"hello"}}, Source: "manual"}}}
	r, e := executeTPM(context.Background(), m, []string{"lv"}, nil)
	if e != nil || !strings.Contains(r.Text, "hello") || !strings.Contains(r.Text, "手动") {
		t.Fatal(r, e)
	}
	r, e = executeTPM(context.Background(), m, []string{"un", "all"}, nil)
	if e != nil || strings.Join(m.calls, ",") != "remove:demo" || !strings.Contains(r.Text, "数据保留") {
		t.Fatal(r, e, m.calls)
	}
}

func TestTPMInstallAllSkipsAlreadyInstalled(t *testing.T) {
	m := &memoryTPM{catalog: []plugin.CatalogEntry{{Manifest: plugin.Manifest{Name: "old"}}, {Manifest: plugin.Manifest{Name: "new"}}}, installed: []plugin.InstalledInfo{{Manifest: plugin.Manifest{Name: "old"}, Source: "manual"}}}
	r, e := executeTPM(context.Background(), m, []string{"i", "all"}, nil)
	if e != nil || strings.Join(m.calls, ",") != "install:new" || !strings.Contains(r.Text, "跳过 1") {
		t.Fatal(r, e, m.calls)
	}
}
func TestTPMUploadAndLocalPath(t *testing.T) {
	m := &memoryTPM{}
	r, e := executeTPM(context.Background(), m, []string{"ul", "demo"}, nil)
	if e != nil || r.FileName != "demo.zip" || string(r.File) != "zip" {
		t.Fatal(r, e)
	}
	_, e = executeTPM(context.Background(), m, []string{"replace", "/path/with space"}, nil)
	if e != nil || m.calls[1] != "replace:/path/with space" {
		t.Fatal(e, m.calls)
	}
}
func TestTPMCancellationPreventsMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &memoryTPM{}
	if _, e := executeTPM(ctx, m, []string{"rm", "one"}, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if len(m.calls) != 0 {
		t.Fatal(m.calls)
	}
}
func TestTPMHasContextualHelp(t *testing.T) {
	a := fixture(t, nil)
	defer a.Close()
	if e := Register(a); e != nil {
		t.Fatal(e)
	}
	c, ok := a.Registry.Lookup("tpm")
	if !ok {
		t.Fatal("missing tpm")
	}
	for _, text := range []string{"，tpm search", "，tpm i", "，tpm rm", "，tpm upload"} {
		if !strings.Contains(c.HelpText("，"), text) {
			t.Fatal(c.HelpText("，"))
		}
	}
}

func TestTPMBrokenManifestDoesNotBlockBatch(t *testing.T) {
	m := &memoryTPM{installed: []plugin.InstalledInfo{{Manifest: plugin.Manifest{Name: "broken"}, Source: plugin.CatalogURL, Modified: true, Error: "manifest missing"}, {Manifest: plugin.Manifest{Name: "healthy", Version: "1"}, Source: plugin.CatalogURL}}}
	r, e := executeTPM(context.Background(), m, []string{"ls"}, nil)
	if e != nil || !strings.Contains(r.Text, "manifest missing") {
		t.Fatal(r, e)
	}
	r, e = executeTPM(context.Background(), m, []string{"ua", "-f"}, nil)
	if e != nil || strings.Join(m.calls, ",") != "force:broken,force:healthy" || !strings.Contains(r.Text, "成功 2") {
		t.Fatal(r, e, m.calls)
	}
}

func TestTPMUpdateBuiltinsAndUnknownHaveDistinctGuidance(t *testing.T) {
	a := fixture(t, nil)
	a.Registry.Register(&command.Command{Name: "yvlu"})
	m := &memoryTPM{}
	managed := managedTPM{tpmManager: m, registry: a.Registry}
	if e := managed.UpdateRemoteForce(t.Context(), "yvlu", false); e == nil || !strings.Contains(e.Error(), "update run") {
		t.Fatal(e)
	}
	if e := managed.UpdateRemoteForce(t.Context(), "missing", false); e == nil || !strings.Contains(e.Error(), "未安装") {
		t.Fatal(e)
	}
	if len(m.calls) != 0 {
		t.Fatal(m.calls)
	}
}
