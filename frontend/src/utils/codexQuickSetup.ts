import type { GroupPlatform } from '@/types'
import { clientV1BaseUrl, normalizeClientBaseUrl, selectClientSetupModel } from '@/utils/clientSetupModels'

export type CodexQuickSetupPlatform = 'windows' | 'macos' | 'linux'

export interface CodexQuickSetupOptions {
  apiKey: string
  baseUrl: string
  platform: CodexQuickSetupPlatform
  groupPlatform?: GroupPlatform | null
  availableModels?: readonly string[]
  model?: string
}

export interface CodexQuickSetupScript {
  platform: CodexQuickSetupPlatform
  filename: string
  mimeType: string
  content: string
}

type BuildAllOptions = Omit<CodexQuickSetupOptions, 'platform'>

const PROVIDER_ID = 'oreniva'

export function buildCodexQuickSetupScripts(options: BuildAllOptions): Record<CodexQuickSetupPlatform, CodexQuickSetupScript> {
  return {
    windows: buildCodexQuickSetupScript({ ...options, platform: 'windows' }),
    macos: buildCodexQuickSetupScript({ ...options, platform: 'macos' }),
    linux: buildCodexQuickSetupScript({ ...options, platform: 'linux' }),
  }
}

export function buildCodexQuickSetupScript(options: CodexQuickSetupOptions): CodexQuickSetupScript {
  const normalizedOptions = {
    ...options,
    apiKey: options.apiKey.trim(),
    baseUrl: options.groupPlatform === 'anthropic' ? normalizeClientBaseUrl(options.baseUrl) : clientV1BaseUrl(options.baseUrl),
    model: selectClientSetupModel(options.groupPlatform, options.availableModels, options.model),
  }

  if (normalizedOptions.groupPlatform === 'typesafe') {
    throw new Error('TypeSafe uses /v1/systemone; open Use Key for its supported request format')
  }

  if (normalizedOptions.groupPlatform === 'anthropic') {
    return buildClaudeCodeQuickSetupScript(normalizedOptions)
  }

  return buildCodexCliQuickSetupScript(normalizedOptions)
}

function buildCodexCliQuickSetupScript(options: CodexQuickSetupOptions): CodexQuickSetupScript {
  if (options.platform === 'windows') {
    return {
      platform: 'windows',
      filename: 'codex-quick-setup-windows.bat',
      mimeType: 'application/x-bat',
      content: buildCodexWindowsBatchScript(options),
    }
  }

  return {
    platform: options.platform,
    filename: `codex-quick-setup-${options.platform}.sh`,
    mimeType: 'application/x-sh',
    content: buildCodexPosixShellScript(options),
  }
}

function buildClaudeCodeQuickSetupScript(options: CodexQuickSetupOptions): CodexQuickSetupScript {
  if (options.platform === 'windows') {
    return {
      platform: 'windows',
      filename: 'claude-quick-setup-windows.bat',
      mimeType: 'application/x-bat',
      content: buildClaudeWindowsBatchScript(options),
    }
  }

  return {
    platform: options.platform,
    filename: `claude-quick-setup-${options.platform}.sh`,
    mimeType: 'application/x-sh',
    content: buildClaudePosixShellScript(options),
  }
}

function jsonString(value: string): string {
  return JSON.stringify(value)
}

function tomlString(value: string): string {
  return JSON.stringify(value)
}

function shellSingleQuoted(value: string): string {
  return `'${value.replace(/'/g, `'"'"'`)}'`
}

function powershellSingleQuoted(value: string): string {
  return `'${value.replace(/'/g, "''")}'`
}

function windowsBatchInstaller(directory: string, files: Record<string, string>): string {
  // Only this fixed, short bootstrap is parsed by CMD. The readable PowerShell
  // body is loaded explicitly as UTF-8 and is never parsed or expanded by CMD.
  const loader = [
    "$ErrorActionPreference = 'Stop'",
    'try {',
    '$source = [IO.File]::ReadAllText($env:SUB2API_SETUP_FILE, [Text.Encoding]::UTF8)',
    "$parts = $source -split '(?m)^# SUB2API_POWERSHELL_BODY\\r?$', 2",
    "if ($parts.Count -ne 2) { throw 'Setup script body is missing.' }",
    '& ([ScriptBlock]::Create($parts[1]))',
    "} catch { [Console]::Error.WriteLine('Unable to read or run the setup script. Download it again and check folder permissions.'); exit 1 }",
  ].join('; ')
  const powershell = [
    '# SUB2API_POWERSHELL_BODY',
    '# Readable configuration. Keep this file private: it contains your API key.',
    "$ErrorActionPreference = 'Stop'",
    '$utf8 = New-Object Text.UTF8Encoding($false)',
    '[Console]::OutputEncoding = $utf8',
    '$files = [ordered]@{',
    ...Object.entries(files).map(([name, content]) => `  ${powershellSingleQuoted(name)} = ${powershellSingleQuoted(content)}`),
    '}',
    "$stage = 'create the configuration directory'",
    'try {',
    `  $target = Join-Path $env:USERPROFILE '${directory}'`,
    '  [void][IO.Directory]::CreateDirectory($target)',
    "  $stamp = (Get-Date).ToString('yyyyMMdd_HHmmss_fffffff') + '_' + [Guid]::NewGuid().ToString('N')",
    "  $backup = Join-Path $target ('backup/' + $stamp)",
    "  $stage = 'back up existing configuration files'",
    '  foreach ($file in $files.GetEnumerator()) {',
    '    $path = Join-Path $target $file.Key',
    '    if (Test-Path -LiteralPath $path) {',
    '      [void][IO.Directory]::CreateDirectory($backup)',
    "      Copy-Item -LiteralPath $path -Destination (Join-Path $backup ($file.Key + '.bak')) -ErrorAction Stop",
    '    }',
    '  }',
    "  $stage = 'write the new configuration files'",
    '  foreach ($file in $files.GetEnumerator()) {',
    '    [IO.File]::WriteAllText((Join-Path $target $file.Key), [string]$file.Value, $utf8)',
    '  }',
    '  Write-Host "Configuration saved to $target"',
    '  if (Test-Path -LiteralPath $backup) { Write-Host "Backup: $backup" }',
    '  Write-Host "Configuration complete. Restart the client to reload settings."',
    '} catch {',
    '  [Console]::Error.WriteLine("Setup failed while trying to $stage. Check folder permissions and available disk space.")',
    '  exit 1',
    '}',
  ].join('\n')
  return [
    '@echo off',
    'setlocal EnableExtensions DisableDelayedExpansion',
    'rem Double-click to configure the client. Existing files are backed up first.',
    'set "SUB2API_SETUP_FILE=%~f0"',
    `powershell.exe -NoLogo -NoProfile -NonInteractive -Command "${loader}"`,
    'set "SUB2API_SETUP_EXIT=%ERRORLEVEL%"',
    'if not "%SUB2API_SETUP_EXIT%"=="0" echo Setup failed. See the error above.',
    'pause',
    'exit /b %SUB2API_SETUP_EXIT%',
    '',
    powershell,
    ''
  ].join('\n').replace(/\r?\n/g, '\r\n')
}

function configToml(options: CodexQuickSetupOptions): string {
  return [
    `model_provider = ${tomlString(PROVIDER_ID)}`,
    `model = ${tomlString(options.model!)}`,
    `review_model = ${tomlString(options.model!)}`,
    'disable_response_storage = true',
    'network_access = "enabled"',
    'windows_wsl_setup_acknowledged = true',
    '',
    `[model_providers.${PROVIDER_ID}]`,
    `name = ${tomlString(PROVIDER_ID)}`,
    `base_url = ${tomlString(options.baseUrl)}`,
    'wire_api = "responses"',
    'requires_openai_auth = false',
    `experimental_bearer_token = ${tomlString(options.apiKey)}`,
    'supports_websockets = false',
    '',
  ].join('\n')
}

function authJson(apiKey: string): string {
  return [
    '{',
    `  "OPENAI_API_KEY": ${jsonString(apiKey)}`,
    '}',
    '',
  ].join('\n')
}

function buildCodexWindowsBatchScript(options: CodexQuickSetupOptions): string {
  return windowsBatchInstaller('.codex', { 'config.toml': configToml(options), 'auth.json': authJson(options.apiKey) })
}

function buildCodexPosixShellScript(options: CodexQuickSetupOptions): string {
  const platformLabel = options.platform === 'macos' ? 'macOS' : 'Linux'
  const lines = [
    '#!/usr/bin/env bash',
    'set -euo pipefail',
    'umask 077',
    '',
    'CODEX_DIR="${HOME}/.codex"',
    'CONFIG_FILE="${CODEX_DIR}/config.toml"',
    'AUTH_FILE="${CODEX_DIR}/auth.json"',
    `BASE_URL=${shellSingleQuoted(options.baseUrl)}`,
    '',
    `echo "========== Codex quick setup (${platformLabel}) =========="`,
    'echo "Target directory: ${CODEX_DIR}"',
    'echo "API endpoint: ${BASE_URL}"',
    'echo',
    '',
    'mkdir -p "${CODEX_DIR}"',
    'STAMP="$(date +%Y%m%d_%H%M%S)_$$"',
    'BACKUP_DIR=""',
    '',
    'if [[ -f "${CONFIG_FILE}" || -f "${AUTH_FILE}" ]]; then',
    '  BACKUP_DIR="${CODEX_DIR}/backup/${STAMP}"',
    '  mkdir -p "${BACKUP_DIR}"',
    '  if [[ -f "${CONFIG_FILE}" ]]; then cp "$CONFIG_FILE" "$BACKUP_DIR/config.toml.bak"; fi',
    '  if [[ -f "${AUTH_FILE}" ]]; then cp "$AUTH_FILE" "$BACKUP_DIR/auth.json.bak"; fi',
    '  echo "Backup directory: ${BACKUP_DIR}"',
    'else',
    '  echo "No existing Codex config files to back up."',
    'fi',
    '',
    'cat > "${CONFIG_FILE}" <<\'CODEX_CONFIG_TOML\'',
    configToml(options).trimEnd(),
    'CODEX_CONFIG_TOML',
    '',
    'cat > "${AUTH_FILE}" <<\'CODEX_AUTH_JSON\'',
    authJson(options.apiKey).trimEnd(),
    'CODEX_AUTH_JSON',
    '',
    'echo',
    'echo "Codex configuration complete. Restart Codex CLI or editor integrations to reload settings."',
    'echo "Config: ${CONFIG_FILE}"',
    'echo "Auth: ${AUTH_FILE}"',
    'if [[ -n "${BACKUP_DIR}" ]]; then',
    '  echo "Backup: ${BACKUP_DIR}"',
    'fi',
    '',
  ]

  return lines.join('\n')
}

function claudeSettingsJson(options: CodexQuickSetupOptions): string {
  const model = options.model!
  const opus = selectClientSetupModel('anthropic', options.availableModels, 'claude-opus-5-5')
  // Only configure this provider. Permission choices remain with the client/user.
  return `${JSON.stringify({
    env: {
      ANTHROPIC_AUTH_TOKEN: options.apiKey,
      ANTHROPIC_BASE_URL: options.baseUrl,
      ANTHROPIC_DEFAULT_HAIKU_MODEL: model,
      ANTHROPIC_DEFAULT_OPUS_MODEL: opus,
      ANTHROPIC_DEFAULT_SONNET_MODEL: model,
      ANTHROPIC_MODEL: model,
      ANTHROPIC_REASONING_MODEL: opus,
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
    },
    model
  }, null, 2)}\n`
}

function buildClaudeWindowsBatchScript(options: CodexQuickSetupOptions): string {
  return windowsBatchInstaller('.claude', { 'settings.json': claudeSettingsJson(options) })
}

function buildClaudePosixShellScript(options: CodexQuickSetupOptions): string {
  const platformLabel = options.platform === 'macos' ? 'macOS' : 'Linux'
  const lines = [
    '#!/usr/bin/env bash',
    'set -euo pipefail',
    'umask 077',
    '',
    'CLAUDE_DIR="${HOME}/.claude"',
    'SETTINGS_FILE="${CLAUDE_DIR}/settings.json"',
    `BASE_URL=${shellSingleQuoted(options.baseUrl)}`,
    '',
    `echo "========== Claude Code quick setup (${platformLabel}) =========="`,
    'echo "Target directory: ${CLAUDE_DIR}"',
    'echo "API endpoint: ${BASE_URL}"',
    'echo',
    '',
    'mkdir -p "${CLAUDE_DIR}"',
    'STAMP="$(date +%Y%m%d_%H%M%S)_$$"',
    'BACKUP_DIR=""',
    '',
    'if [[ -f "${SETTINGS_FILE}" ]]; then',
    '  BACKUP_DIR="${CLAUDE_DIR}/backup/${STAMP}"',
    '  mkdir -p "${BACKUP_DIR}"',
    '  cp "$SETTINGS_FILE" "$BACKUP_DIR/settings.json.bak"',
    '  echo "Backup directory: ${BACKUP_DIR}"',
    'else',
    '  echo "No existing Claude settings file to back up."',
    'fi',
    '',
    'cat > "${SETTINGS_FILE}" <<\'CLAUDE_SETTINGS_JSON\'',
    claudeSettingsJson(options).trimEnd(),
    'CLAUDE_SETTINGS_JSON',
    '',
    'echo',
    'echo "Claude Code configuration complete. Restart Claude Code to reload settings."',
    'echo "Settings: ${SETTINGS_FILE}"',
    'if [[ -n "${BACKUP_DIR}" ]]; then',
    '  echo "Backup: ${BACKUP_DIR}"',
    'fi',
    '',
  ]

  return lines.join('\n')
}
