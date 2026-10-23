package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
)

const skillInstallTokenTTL = 15 * time.Minute

// Install target folders, relative to the user's home directory. Anything else is rejected.
var skillInstallDirs = map[string]bool{
	".claude/skills": true,
	".cursor/skills": true,
	".codex/skills":  true,
	".agents/skills": true,
}

// ponytail: in-memory tokens, single replica; move to DB if Obot is scaled out.
// Tokens can be reused until they expire so users can retry a failed install.
type installTokenStore struct {
	mu     sync.Mutex
	ttl    time.Duration
	now    func() time.Time
	tokens map[string]installToken
}

type installToken struct {
	skillID, userID string
	expires         time.Time
}

func newInstallTokenStore(ttl time.Duration) *installTokenStore {
	return &installTokenStore{ttl: ttl, now: time.Now, tokens: map[string]installToken{}}
}

func (s *installTokenStore) issue(skillID, userID string) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate install token: %w", err)
	}
	token := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.tokens {
		if !now.Before(v.expires) {
			delete(s.tokens, k)
		}
	}
	expires := now.Add(s.ttl)
	s.tokens[token] = installToken{skillID: skillID, userID: userID, expires: expires}
	return token, expires, nil
}

// lookup returns the skill ID the token was issued for, or false if it is unknown or expired.
func (s *installTokenStore) lookup(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[token]
	if !ok || !s.now().Before(t.expires) {
		return "", false
	}
	return t.skillID, true
}

// InstallToken issues a short-lived token that lets a terminal without a login
// download this skill. Access is checked by authz exactly like GET /download.
func (h *SkillHandler) InstallToken(req api.Context) error {
	skill, err := h.getAccessibleSkill(req, req.PathValue("id"))
	if err != nil {
		return err
	}
	token, expires, err := h.installTokens.issue(skill.Name, req.User.GetUID())
	if err != nil {
		return err
	}
	return req.Write(map[string]any{"token": token, "expiresAt": expires.UTC()})
}

// InstallScript is public: it returns a shell or PowerShell script for a valid token.
func (h *SkillHandler) InstallScript(req api.Context) error {
	skill, ok := h.skillForInstallToken(req)
	if !ok {
		return nil
	}

	dir := req.URL.Query().Get("dir")
	if dir == "" {
		dir = ".claude/skills"
	}
	if !skillInstallDirs[dir] {
		http.Error(req.ResponseWriter, "Unsupported install folder. Use one of: .claude/skills, .cursor/skills, .codex/skills, .agents/skills", http.StatusBadRequest)
		return nil
	}

	zipURL := strings.TrimSuffix(req.APIBaseURL, "/") + "/skill-install/" + req.PathValue("token") + "/zip"
	script, err := skillInstallScript(req.PathValue("os"), skillInstallName(skill), dir, zipURL)
	if err != nil {
		http.Error(req.ResponseWriter, err.Error(), http.StatusBadRequest)
		return nil
	}

	req.ResponseWriter.Header().Set("Content-Type", "text/plain; charset=utf-8")
	req.ResponseWriter.Header().Set("Cache-Control", "no-store")
	req.WriteHeader(http.StatusOK)
	_, err = req.ResponseWriter.Write([]byte(script))
	return err
}

// InstallZip is public: it returns the skill ZIP for a valid token.
func (h *SkillHandler) InstallZip(req api.Context) error {
	skill, ok := h.skillForInstallToken(req)
	if !ok {
		return nil
	}
	req.ResponseWriter.Header().Set("Cache-Control", "no-store")
	return h.writeSkillZip(req, skill)
}

// skillForInstallToken writes a plain 404 and returns false when the token or skill is not usable.
func (h *SkillHandler) skillForInstallToken(req api.Context) (*v1.Skill, bool) {
	if skillID, ok := h.installTokens.lookup(req.PathValue("token")); ok {
		if skill, err := h.getAccessibleSkill(req, skillID); err == nil {
			return skill, true
		}
	}
	http.Error(req.ResponseWriter, "This install link is invalid or has expired. Open the Skills page and copy a new command.", http.StatusNotFound)
	return nil, false
}

// skillInstallName is the folder name for the skill. It only contains [a-z0-9_-], so it is safe inside scripts.
func skillInstallName(skill *v1.Skill) string {
	if name := sanitizeDownloadFilename(skill.Spec.Name); name != "" {
		return name
	}
	if name := sanitizeDownloadFilename(skill.Name); name != "" {
		return name
	}
	return "skill"
}

// skillInstallScript builds the install script. name must come from skillInstallName and dir from skillInstallDirs.
func skillInstallScript(osName, name, dir, zipURL string) (string, error) {
	if strings.ContainsAny(zipURL, "'\"`$\\ \n\r") {
		return "", fmt.Errorf("server URL contains characters that are not allowed in an install script")
	}
	switch osName {
	case "windows":
		target := strings.ReplaceAll(dir, "/", `\`) + `\` + name
		return fmt.Sprintf(windowsInstallScript, target, zipURL, name), nil
	case "unix":
		return fmt.Sprintf(unixInstallScript, dir+"/"+name, zipURL, name), nil
	default:
		return "", fmt.Errorf("unknown OS %q, use windows or unix", osName)
	}
}

// Runs inside the caller's session via `irm | iex`, so it is wrapped in a script block
// and uses throw instead of exit (exit would close the user's PowerShell window).
// Existing files are overwritten in place; nothing is deleted.
const windowsInstallScript = `& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  $dest = Join-Path $HOME '%[1]s'
  $zip = Join-Path $env:TEMP ('obot-skill-' + [guid]::NewGuid().ToString() + '.zip')
  try {
    Invoke-WebRequest -UseBasicParsing -Uri '%[2]s' -OutFile $zip
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    Expand-Archive -LiteralPath $zip -DestinationPath $dest -Force
  } catch {
    throw "Install of %[3]s failed: $($_.Exception.Message)"
  } finally {
    if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
  }
  Write-Host "Installed %[3]s to $dest. Restart your AI tool."
}
`

const unixInstallScript = `#!/bin/sh
set -eu
dest="$HOME/%[1]s"
zip=$(mktemp)
trap 'rm -f "$zip"' EXIT
fail() { echo "Install of %[3]s failed: $1" >&2; exit 1; }
curl -fsSL '%[2]s' -o "$zip" || fail "could not download the skill (the link may have expired)"
mkdir -p "$dest"
if command -v unzip >/dev/null 2>&1; then
  unzip -oq "$zip" -d "$dest" || fail "could not unzip"
elif command -v python3 >/dev/null 2>&1; then
  python3 -m zipfile -e "$zip" "$dest" || fail "could not unzip"
elif command -v bsdtar >/dev/null 2>&1; then
  bsdtar -xf "$zip" -C "$dest" || fail "could not unzip"
else
  fail "install unzip or python3 and try again"
fi
echo "Installed %[3]s to $dest. Restart your AI tool."
`
