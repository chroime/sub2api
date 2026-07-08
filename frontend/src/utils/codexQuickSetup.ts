export type CodexQuickSetupPlatform = 'windows' | 'macos' | 'linux'

export interface CodexQuickSetupOptions {
  apiKey: string
  baseUrl: string
  platform: CodexQuickSetupPlatform
}

export interface CodexQuickSetupScript {
  platform: CodexQuickSetupPlatform
  filename: string
  mimeType: string
  content: string
}

type BuildAllOptions = Omit<CodexQuickSetupOptions, 'platform'>

const PROVIDER_ID = 'oreniva'
const MODEL_ID = 'gpt-5.5'

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
    baseUrl: normalizeBaseUrl(options.baseUrl),
  }

  if (normalizedOptions.platform === 'windows') {
    return {
      platform: 'windows',
      filename: 'codex-quick-setup-windows.bat',
      mimeType: 'application/x-bat',
      content: buildWindowsBatchScript(normalizedOptions),
    }
  }

  return {
    platform: normalizedOptions.platform,
    filename: `codex-quick-setup-${normalizedOptions.platform}.sh`,
    mimeType: 'application/x-sh',
    content: buildPosixShellScript(normalizedOptions),
  }
}

function normalizeBaseUrl(baseUrl: string): string {
  return baseUrl.trim().replace(/\/+$/, '')
}

function jsonString(value: string): string {
  return JSON.stringify(value)
}

function tomlString(value: string): string {
  return JSON.stringify(value)
}

function batchValue(value: string): string {
  return value.replace(/%/g, '%%').replace(/"/g, '\\"')
}

function shellSingleQuoted(value: string): string {
  return `'${value.replace(/'/g, `'\"'\"'`)}'`
}

function configToml(baseUrl: string): string {
  return [
    `model_provider = ${tomlString(PROVIDER_ID)}`,
    `model = ${tomlString(MODEL_ID)}`,
    `review_model = ${tomlString(MODEL_ID)}`,
    'model_reasoning_effort = "xhigh"',
    'disable_response_storage = true',
    'network_access = "enabled"',
    'windows_wsl_setup_acknowledged = true',
    'model_context_window = 1000000',
    'model_auto_compact_token_limit = 900000',
    '',
    `[model_providers.${PROVIDER_ID}]`,
    `name = ${tomlString(PROVIDER_ID)}`,
    `base_url = ${tomlString(baseUrl)}`,
    'wire_api = "responses"',
    'requires_openai_auth = true',
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

function buildWindowsBatchScript(options: CodexQuickSetupOptions): string {
  const configLines = configToml(options.baseUrl).split('\n')
  const authLines = authJson(options.apiKey).split('\n')
  const lines = [
    '@echo off',
    'setlocal EnableExtensions DisableDelayedExpansion',
    'chcp 65001 >nul 2>nul',
    '',
    'set "CODEX_DIR=%USERPROFILE%\\.codex"',
    'set "CONFIG_FILE=%CODEX_DIR%\\config.toml"',
    'set "AUTH_FILE=%CODEX_DIR%\\auth.json"',
    `set "API_KEY=${batchValue(options.apiKey)}"`,
    `set "BASE_URL=${batchValue(options.baseUrl)}"`,
    '',
    'echo.',
    'echo ========== Codex quick setup ==========',
    'echo Target directory: "%CODEX_DIR%"',
    'echo API endpoint: "%BASE_URL%"',
    'echo.',
    '',
    'if not exist "%CODEX_DIR%" mkdir "%CODEX_DIR%"',
    'if errorlevel 1 (',
    '    echo Failed to create "%CODEX_DIR%".',
    '    exit /b 1',
    ')',
    '',
    'for /f "usebackq delims=" %%I in (`powershell -NoProfile -Command "(Get-Date).ToString(\'yyyyMMdd_HHmmss\')"`) do set "STAMP=%%I"',
    'set "BACKUP_DIR="',
    'if exist "%CONFIG_FILE%" set "BACKUP_DIR=%CODEX_DIR%\\backup\\%STAMP%"',
    'if exist "%AUTH_FILE%" set "BACKUP_DIR=%CODEX_DIR%\\backup\\%STAMP%"',
    '',
    'if defined BACKUP_DIR (',
    '    echo Backing up existing files...',
    '    mkdir "%BACKUP_DIR%" >nul 2>nul',
    '    if exist "%CONFIG_FILE%" copy /Y "%CONFIG_FILE%" "%BACKUP_DIR%\\config.toml.bak" >nul',
    '    if exist "%AUTH_FILE%" copy /Y "%AUTH_FILE%" "%BACKUP_DIR%\\auth.json.bak" >nul',
    '    echo Backup directory: "%BACKUP_DIR%"',
    ') else (',
    '    echo No existing Codex config files to back up.',
    ')',
    '',
    'echo Writing config.toml...',
    '> "%CONFIG_FILE%" (',
    ...configLines.map((line) => (line ? `    echo ${line}` : '    echo.')),
    ')',
    'if errorlevel 1 (',
    '    echo Failed to write config.toml.',
    '    exit /b 1',
    ')',
    '',
    'echo Writing auth.json...',
    '> "%AUTH_FILE%" (',
    ...authLines.map((line) => (line ? `    echo ${line}` : '    echo.')),
    ')',
    'if errorlevel 1 (',
    '    echo Failed to write auth.json.',
    '    exit /b 1',
    ')',
    '',
    'echo.',
    'echo Codex configuration complete. Restart Codex CLI or editor integrations to reload settings.',
    'echo Config: "%CONFIG_FILE%"',
    'echo Auth: "%AUTH_FILE%"',
    'if defined BACKUP_DIR echo Backup: "%BACKUP_DIR%"',
    'echo.',
    'pause',
    'exit /b 0',
    '',
  ]

  return lines.join('\r\n')
}

function buildPosixShellScript(options: CodexQuickSetupOptions): string {
  const platformLabel = options.platform === 'macos' ? 'macOS' : 'Linux'
  const lines = [
    '#!/usr/bin/env bash',
    'set -euo pipefail',
    '',
    'CODEX_DIR="${HOME}/.codex"',
    'CONFIG_FILE="${CODEX_DIR}/config.toml"',
    'AUTH_FILE="${CODEX_DIR}/auth.json"',
    `API_KEY=${shellSingleQuoted(options.apiKey)}`,
    `BASE_URL=${shellSingleQuoted(options.baseUrl)}`,
    '',
    `echo "========== Codex quick setup (${platformLabel}) =========="`,
    'echo "Target directory: ${CODEX_DIR}"',
    'echo "API endpoint: ${BASE_URL}"',
    'echo',
    '',
    'mkdir -p "${CODEX_DIR}"',
    'STAMP="$(date +%Y%m%d_%H%M%S)"',
    'BACKUP_DIR=""',
    '',
    'if [[ -f "${CONFIG_FILE}" || -f "${AUTH_FILE}" ]]; then',
    '  BACKUP_DIR="${CODEX_DIR}/backup/${STAMP}"',
    '  mkdir -p "${BACKUP_DIR}"',
    '  [[ -f "${CONFIG_FILE}" ]] && cp "$CONFIG_FILE" "$BACKUP_DIR/config.toml.bak"',
    '  [[ -f "${AUTH_FILE}" ]] && cp "$AUTH_FILE" "$BACKUP_DIR/auth.json.bak"',
    '  echo "Backup directory: ${BACKUP_DIR}"',
    'else',
    '  echo "No existing Codex config files to back up."',
    'fi',
    '',
    'cat > "${CONFIG_FILE}" <<\'CODEX_CONFIG_TOML\'',
    configToml(options.baseUrl).trimEnd(),
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
