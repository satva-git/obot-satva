package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kuser "k8s.io/apiserver/pkg/authentication/user"
)

func TestInstallTokenStore(t *testing.T) {
	now := time.Unix(1000, 0)
	store := newInstallTokenStore(15 * time.Minute)
	store.now = func() time.Time { return now }

	token, expires, err := store.issue("sk1", "user1")
	require.NoError(t, err)
	assert.Len(t, token, 64)
	assert.Equal(t, now.Add(15*time.Minute), expires)

	other, _, err := store.issue("sk1", "user1")
	require.NoError(t, err)
	assert.NotEqual(t, token, other)

	got, ok := store.lookup(token)
	assert.True(t, ok)
	assert.Equal(t, "sk1", got)
	_, ok = store.lookup(token) // reusable until expiry
	assert.True(t, ok)

	_, ok = store.lookup("wrong")
	assert.False(t, ok)

	now = now.Add(15 * time.Minute)
	_, ok = store.lookup(token)
	assert.False(t, ok, "expired token must not work")

	_, _, err = store.issue("sk2", "user1")
	require.NoError(t, err)
	assert.Len(t, store.tokens, 1, "expired tokens are pruned on issue")
}

func TestSkillInstallName(t *testing.T) {
	assert.Equal(t, "my-skill-touch-home-x", skillInstallName(&v1.Skill{Spec: v1.SkillSpec{SkillManifest: types.SkillManifest{Name: "My Skill'\"; touch $HOME/x`"}}}))
	assert.Equal(t, "sk1", skillInstallName(&v1.Skill{ObjectMeta: metav1.ObjectMeta{Name: "sk1"}, Spec: v1.SkillSpec{SkillManifest: types.SkillManifest{Name: "$$$"}}}))
}

func TestSkillInstallKind(t *testing.T) {
	kind := func(k string) string {
		return skillInstallKind(&v1.Skill{Spec: v1.SkillSpec{SkillManifest: types.SkillManifest{MetadataValues: map[string]string{"kind": k}}}})
	}
	assert.Equal(t, "agent", kind("agent"))
	assert.Equal(t, "guardrail", kind("guardrail"))
	assert.Equal(t, "skill", kind("'; rm x"))
	assert.Equal(t, "skill", skillInstallKind(&v1.Skill{}))
}

func TestSkillInstallScript(t *testing.T) {
	url := "https://obot.example.com/api/skill-install/abc/zip"

	ps, err := skillInstallScript("windows", "skill", "pg-helper", ".cursor/skills", url)
	require.NoError(t, err)
	assert.Contains(t, ps, `Join-Path $HOME '.cursor\skills\pg-helper'`)
	assert.Contains(t, ps, "Invoke-WebRequest -UseBasicParsing -Uri '"+url+"'")
	assert.Contains(t, ps, "Installed pg-helper to $dest. Restart your AI tool.")

	sh, err := skillInstallScript("unix", "skill", "pg-helper", ".claude/skills", url)
	require.NoError(t, err)
	assert.Contains(t, sh, `dest="$HOME/.claude/skills/pg-helper"`)
	assert.Contains(t, sh, "curl -fsSL '"+url+"'")

	ps, err = skillInstallScript("windows", "agent", "data-analyst-agent", ".claude/skills", url)
	require.NoError(t, err)
	assert.Contains(t, ps, "$kind = 'agent'")
	assert.Contains(t, ps, `Join-Path $HOME '.claude\agents'`)

	sh, err = skillInstallScript("unix", "guardrail", "org-guardrails", ".claude/skills", url)
	require.NoError(t, err)
	assert.Contains(t, sh, "kind='guardrail'")
	assert.Contains(t, sh, `sudo "$node" "$dest/install.mjs"`)
	assert.NotContains(t, sh, "{{")

	_, err = skillInstallScript("plan9", "skill", "pg-helper", ".claude/skills", url)
	assert.Error(t, err)
	_, err = skillInstallScript("unix", "skill", "pg-helper", ".claude/skills", "https://x/'$(id)'")
	assert.Error(t, err)
}

// TestSkillInstallFlow issues a token as a logged-in user, then fetches the script and ZIP
// with no user, and (when the tools exist) runs the real script against a fake server.
func TestSkillInstallFlow(t *testing.T) {
	skill := &v1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "sk1", Namespace: system.DefaultNamespace},
		Spec:       v1.SkillSpec{SkillManifest: types.SkillManifest{Name: "postgres-helper"}, RepoID: "repo-1"},
		Status:     v1.SkillStatus{Valid: true},
	}
	storage := newFakeStorage(t, skill)
	gatewayClient := newHandlerTestGateway(t)

	skillDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: postgres-helper\ndescription: Test\n---\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("echo hi\n"), 0o644))

	handler := NewSkillHandler(nil)
	handler.materializeSkillSource = func(context.Context, *v1.Skill, string) (func(), string, error) {
		return func() {}, skillDir, nil
	}

	var baseURL string
	mux := http.NewServeMux()
	wrap := func(fn func(api.Context) error, u kuser.Info) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			err := fn(api.Context{ResponseWriter: w, Request: r, Storage: storage, GatewayClient: gatewayClient, User: u, APIBaseURL: baseURL + "/api"})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	}
	anonymous := &kuser.DefaultInfo{Name: "system:anonymous"}
	mux.HandleFunc("POST /api/skills/{id}/install-token", wrap(handler.InstallToken, testUser("user1")))
	mux.HandleFunc("GET /api/skill-install/{token}/zip", wrap(handler.InstallZip, anonymous))
	mux.HandleFunc("GET /api/skill-install/{token}/{os}", wrap(handler.InstallScript, anonymous))
	server := httptest.NewServer(mux)
	defer server.Close()
	baseURL = server.URL

	resp, err := http.Post(server.URL+"/api/skills/sk1/install-token", "", nil)
	require.NoError(t, err)
	var body struct{ Token string }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	require.Len(t, body.Token, 64)
	tokenURL := server.URL + "/api/skill-install/" + body.Token

	for path, want := range map[string]int{
		"/windows?dir=.claude/skills": http.StatusOK,
		"/unix?dir=.codex/skills":     http.StatusOK,
		"/zip":                        http.StatusOK,
		"/unix?dir=../../etc":         http.StatusBadRequest,
		"/mac":                        http.StatusBadRequest,
	} {
		resp, err := http.Get(tokenURL + path)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, want, resp.StatusCode, path)
	}
	resp, err = http.Get(server.URL + "/api/skill-install/deadbeef/unix")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	home := t.TempDir()
	if runtime.GOOS == "windows" {
		if ps, err := exec.LookPath("powershell.exe"); err == nil {
			cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", `irm "`+tokenURL+`/windows?dir=.claude/skills" | iex`)
			cmd.Env = append(os.Environ(), "USERPROFILE="+home, "HOMEDRIVE=", "HOMEPATH=")
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			t.Log(string(out))
			assert.Contains(t, string(out), "Installed postgres-helper to")
			assert.FileExists(t, filepath.Join(home, ".claude", "skills", "postgres-helper", "SKILL.md"))
			assert.FileExists(t, filepath.Join(home, ".claude", "skills", "postgres-helper", "scripts", "run.sh"))
		}
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		if _, err := exec.LookPath("curl"); err == nil {
			cmd := exec.Command(sh, "-c", `curl -fsSL "`+tokenURL+`/unix?dir=.agents/skills" | sh`)
			cmd.Env = append(os.Environ(), "HOME="+filepath.ToSlash(home))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			t.Log(string(out))
			assert.Contains(t, string(out), "Installed postgres-helper to")
			assert.FileExists(t, filepath.Join(home, ".agents", "skills", "postgres-helper", "SKILL.md"))
			assert.FileExists(t, filepath.Join(home, ".agents", "skills", "postgres-helper", "scripts", "run.sh"))
		}
	}
}
