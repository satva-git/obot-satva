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
	script, err := skillInstallScript(req.PathValue("os"), skillInstallKind(skill), skillInstallName(skill), dir, zipURL)
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

// skillInstallKind reads the "kind" the skills repo puts in SKILL.md metadata. Agents are copied into the
// agents folder, guardrails also run their installer (install.mjs), anything else is a plain skill.
func skillInstallKind(skill *v1.Skill) string {
	switch k := skill.Spec.MetadataValues["kind"]; k {
	case "agent", "guardrail":
		return k
	}
	return "skill"
}

// skillInstallScript builds the install script. name must come from skillInstallName, dir from
// skillInstallDirs and kind from skillInstallKind, so none of them can break out of the quotes.
func skillInstallScript(osName, kind, name, dir, zipURL string) (string, error) {
	if strings.ContainsAny(zipURL, "'\"`$\\ \n\r") {
		return "", fmt.Errorf("server URL contains characters that are not allowed in an install script")
	}
	agents := strings.TrimSuffix(dir, "/skills") + "/agents"
	var script string
	switch osName {
	case "windows":
		script, dir, agents = windowsInstallScript, strings.ReplaceAll(dir, "/", `\`)+`\`+name, strings.ReplaceAll(agents, "/", `\`)
	case "unix":
		script, dir = unixInstallScript, dir+"/"+name
	default:
		return "", fmt.Errorf("unknown OS %q, use windows or unix", osName)
	}
	return strings.NewReplacer("{{KIND}}", kind, "{{NAME}}", name, "{{DEST}}", dir, "{{AGENTS}}", agents, "{{URL}}", zipURL).Replace(script), nil
}

// Runs inside the caller's session via `irm | iex`, so it is wrapped in a script block
// and uses throw instead of exit (exit would close the user's PowerShell window).
// Existing files are overwritten in place; nothing in the install folder is deleted.
const windowsInstallScript = `& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  $kind = '{{KIND}}'
  $dest = Join-Path $HOME '{{DEST}}'
  $zip = Join-Path $env:TEMP ('obot-skill-' + [guid]::NewGuid().ToString() + '.zip')
  $tmp = $zip + '.d'
  try {
    Invoke-WebRequest -UseBasicParsing -Uri '{{URL}}' -OutFile $zip
    if ($kind -eq 'agent') {
      # The zip holds SKILL.md (the page entry) plus the agent file; only the agent goes into the agents folder.
      Expand-Archive -LiteralPath $zip -DestinationPath $tmp -Force
      $dest = Join-Path $HOME '{{AGENTS}}'
      New-Item -ItemType Directory -Force -Path $dest | Out-Null
      Get-ChildItem -LiteralPath $tmp -Filter *.md | Where-Object { $_.Name -ne 'SKILL.md' } | Copy-Item -Destination $dest -Force
    } else {
      New-Item -ItemType Directory -Force -Path $dest | Out-Null
      Expand-Archive -LiteralPath $zip -DestinationPath $dest -Force
    }
    $installer = Join-Path $dest 'install.mjs'
    if ($kind -eq 'guardrail' -and (Test-Path -LiteralPath $installer)) {
      if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'the guardrails need Node.js 18 or newer. Install it from https://nodejs.org and run this command again' }
      $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
      if ($admin) {
        & node $installer
        if ($LASTEXITCODE) { throw 'the guardrail installer failed, see the message above' }
      } else {
        Write-Host 'Guardrails are installed for every user of this PC, so Windows will ask for admin rights. Click Yes.'
        $p = Start-Process node -ArgumentList ('"' + $installer + '"') -Verb RunAs -Wait -PassThru
        if ($p.ExitCode) { throw "the guardrail installer failed (exit $($p.ExitCode)). Run PowerShell as Administrator and try again" }
      }
      & node $installer --verify
      if ($LASTEXITCODE) { throw 'the guardrail check failed, see the FAIL lines above' }
    }
  } catch {
    throw "Install of {{NAME}} failed: $($_.Exception.Message)"
  } finally {
    if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
    if (Test-Path -LiteralPath $tmp) { Get-ChildItem -LiteralPath $tmp | Remove-Item -Force; Remove-Item -LiteralPath $tmp -Force }
  }
  Write-Host "Installed {{NAME}} to $dest. Restart your AI tool."
}
`

// Agent zips are flat (SKILL.md + the agent file), so the temp folder never holds subfolders.
const unixInstallScript = `#!/bin/sh
set -eu
kind='{{KIND}}'
dest="$HOME/{{DEST}}"
tmp=$(mktemp -d)
trap 'rm -f "$tmp"/*; rmdir "$tmp"' EXIT
fail() { echo "Install of {{NAME}} failed: $1" >&2; exit 1; }
unpack() {
  mkdir -p "$1"
  if command -v unzip >/dev/null 2>&1; then
    unzip -oq "$tmp/skill.zip" -d "$1" || fail "could not unzip"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -m zipfile -e "$tmp/skill.zip" "$1" || fail "could not unzip"
  elif command -v bsdtar >/dev/null 2>&1; then
    bsdtar -xf "$tmp/skill.zip" -C "$1" || fail "could not unzip"
  else
    fail "install unzip or python3 and try again"
  fi
}
curl -fsSL '{{URL}}' -o "$tmp/skill.zip" || fail "could not download the skill (the link may have expired)"
if [ "$kind" = agent ]; then
  # Only the agent file goes into the agents folder, not SKILL.md (the page entry).
  unpack "$tmp"
  dest="$HOME/{{AGENTS}}"
  mkdir -p "$dest"
  for f in "$tmp"/*.md; do [ "$(basename "$f")" = SKILL.md ] || cp -f "$f" "$dest/"; done
else
  unpack "$dest"
fi
if [ "$kind" = guardrail ] && [ -f "$dest/install.mjs" ]; then
  node=$(command -v node) || fail "the guardrails need Node.js 18 or newer. Install it from https://nodejs.org and run this command again"
  if [ "$(id -u)" = 0 ]; then
    "$node" "$dest/install.mjs" || fail "the guardrail installer failed, see the message above"
  else
    echo "Guardrails are installed for every user of this machine, so sudo will ask for your password."
    sudo "$node" "$dest/install.mjs" || fail "the guardrail installer failed, see the message above"
  fi
  "$node" "$dest/install.mjs" --verify || fail "the guardrail check failed, see the FAIL lines above"
fi
echo "Installed {{NAME}} to $dest. Restart your AI tool."
`
