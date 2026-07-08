import type { GroupPlatform } from '@/types'

export type CodexQuickSetupPlatform = 'windows' | 'macos' | 'linux'

export interface CodexQuickSetupOptions {
  apiKey: string
  baseUrl: string
  platform: CodexQuickSetupPlatform
  groupPlatform?: GroupPlatform | null
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

function batchEchoLine(line: string): string {
  if (!line) return '    echo.'

  const escapedLine = line.replace(/[%^&|<>()]/g, (character) => {
    switch (character) {
      case '%':
        return '%%'
      case '^':
        return '^^'
      default:
        return `^${character}`
    }
  })

  return `    echo ${escapedLine}`
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

function buildCodexWindowsBatchScript(options: CodexQuickSetupOptions): string {
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
    ...configLines.map(batchEchoLine),
    ')',
    'if errorlevel 1 (',
    '    echo Failed to write config.toml.',
    '    exit /b 1',
    ')',
    '',
    'echo Writing auth.json...',
    '> "%AUTH_FILE%" (',
    ...authLines.map(batchEchoLine),
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

function buildCodexPosixShellScript(options: CodexQuickSetupOptions): string {
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

function claudeSettingsJson(baseUrl: string, apiKey: string): string {
  return `${JSON.stringify({
    cleanupPeriodDays: 30,
    env: {
      ANTHROPIC_AUTH_TOKEN: apiKey,
      ANTHROPIC_BASE_URL: baseUrl,
      ANTHROPIC_DEFAULT_HAIKU_MODEL: 'claude-haiku-4-5',
      ANTHROPIC_DEFAULT_OPUS_MODEL: 'claude-opus-4-6',
      ANTHROPIC_DEFAULT_SONNET_MODEL: 'claude-sonnet-4-6',
      ANTHROPIC_MODEL: 'claude-sonnet-4-6',
      ANTHROPIC_REASONING_MODEL: 'claude-opus-4-6',
      API_TIMEOUT_MS: '1200000',
      BASH_DEFAULT_TIMEOUT_MS: '120000',
      BASH_MAX_OUTPUT_LENGTH: '50000',
      BASH_MAX_TIMEOUT_MS: '1200000',
      CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR: '1',
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
      MAX_MCP_OUTPUT_TOKENS: '25000',
      MCP_TIMEOUT: '60000',
      MCP_TOOL_TIMEOUT: '120000',
    },
    includeCoAuthoredBy: false,
    permissions: {
      allow: [
        'Read',
        'Edit',
        'MultiEdit',
        'Write',
        'Glob',
        'Grep',
        'LS',
        'NotebookRead',
        'NotebookEdit',
        'WebFetch',
        'WebSearch',
        'Task',
        'Bash(git status:*)',
        'Bash(git diff:*)',
        'Bash(git log:*)',
        'Bash(git show:*)',
        'Bash(git branch:*)',
        'Bash(git rev-parse:*)',
        'Bash(git ls-files:*)',
        'Bash(npm:*)',
        'Bash(pnpm:*)',
        'Bash(yarn:*)',
        'Bash(node:*)',
        'Bash(npx:*)',
        'Bash(bun:*)',
        'Bash(deno:*)',
        'Bash(python:*)',
        'Bash(py:*)',
        'Bash(pip:*)',
        'Bash(uv:*)',
        'Bash(go:*)',
        'Bash(cargo:*)',
        'Bash(rg:*)',
        'Bash(Get-ChildItem:*)',
        'Bash(Get-Content:*)',
        'Bash(Select-String:*)',
      ],
      deny: [
        'Read(~/.claude/settings.json)',
        'Read(~/.ssh/**)',
        'Read(~/.aws/**)',
        'Read(~/.config/gcloud/**)',
        'Read(./.env)',
        'Read(./.env.*)',
        'Read(./**/.env)',
        'Read(./**/.env.*)',
        'Read(./secrets/**)',
        'Read(./**/secrets/**)',
        'Read(./config/credentials.json)',
        'Read(./**/config/credentials.json)',
        'Bash(git reset --hard:*)',
        'Bash(git clean -fd:*)',
        'Bash(rm -rf:*)',
        'Bash(Remove-Item * -Recurse:*)',
      ],
      ask: [
        'Bash(git fetch:*)',
        'Bash(git pull:*)',
        'Bash(git push:*)',
        'Bash(git merge:*)',
        'Bash(git rebase:*)',
        'Bash(git checkout:*)',
        'Bash(git reset:*)',
        'Bash(git clean:*)',
        'Bash(npm publish:*)',
        'Bash(pnpm publish:*)',
        'Bash(yarn publish:*)',
      ],
      defaultMode: 'bypassPermissions',
      additionalDirectories: ['E:\\workspace'],
      skipDangerousModePermissionPrompt: true,
    },
    model: 'opus[1m]',
    codemossProviderId: '757cc245-ec02-4ec5-a8f4-49e49fd5d0e7',
    skipDangerousModePermissionPrompt: true,
  }, null, 2)}\n`
}

function buildClaudeWindowsBatchScript(options: CodexQuickSetupOptions): string {
  const settingsLines = claudeSettingsJson(options.baseUrl, options.apiKey).split('\n')
  const lines = [
    '@echo off',
    'setlocal EnableExtensions DisableDelayedExpansion',
    'chcp 65001 >nul 2>nul',
    '',
    'set "CLAUDE_DIR=%USERPROFILE%\\.claude"',
    'set "SETTINGS_FILE=%CLAUDE_DIR%\\settings.json"',
    `set "API_KEY=${batchValue(options.apiKey)}"`,
    `set "BASE_URL=${batchValue(options.baseUrl)}"`,
    '',
    'echo.',
    'echo ========== Claude Code quick setup ==========',
    'echo Target directory: "%CLAUDE_DIR%"',
    'echo API endpoint: "%BASE_URL%"',
    'echo.',
    '',
    'if not exist "%CLAUDE_DIR%" mkdir "%CLAUDE_DIR%"',
    'if errorlevel 1 (',
    '    echo Failed to create "%CLAUDE_DIR%".',
    '    exit /b 1',
    ')',
    '',
    'for /f "usebackq delims=" %%I in (`powershell -NoProfile -Command "(Get-Date).ToString(\'yyyyMMdd_HHmmss\')"`) do set "STAMP=%%I"',
    'set "BACKUP_DIR="',
    'if exist "%SETTINGS_FILE%" set "BACKUP_DIR=%CLAUDE_DIR%\\backup\\%STAMP%"',
    '',
    'if defined BACKUP_DIR (',
    '    echo Backing up existing Claude settings...',
    '    mkdir "%BACKUP_DIR%" >nul 2>nul',
    '    if exist "%SETTINGS_FILE%" copy /Y "%SETTINGS_FILE%" "%BACKUP_DIR%\\settings.json.bak" >nul',
    '    echo Backup directory: "%BACKUP_DIR%"',
    ') else (',
    '    echo No existing Claude settings file to back up.',
    ')',
    '',
    'echo Writing settings.json...',
    '> "%SETTINGS_FILE%" (',
    ...settingsLines.map(batchEchoLine),
    ')',
    'if errorlevel 1 (',
    '    echo Failed to write settings.json.',
    '    exit /b 1',
    ')',
    '',
    'echo.',
    'echo Claude Code configuration complete. Restart Claude Code to reload settings.',
    'echo Settings: "%SETTINGS_FILE%"',
    'if defined BACKUP_DIR echo Backup: "%BACKUP_DIR%"',
    'echo.',
    'pause',
    'exit /b 0',
    '',
  ]

  return lines.join('\r\n')
}

function buildClaudePosixShellScript(options: CodexQuickSetupOptions): string {
  const platformLabel = options.platform === 'macos' ? 'macOS' : 'Linux'
  const lines = [
    '#!/usr/bin/env bash',
    'set -euo pipefail',
    '',
    'CLAUDE_DIR="${HOME}/.claude"',
    'SETTINGS_FILE="${CLAUDE_DIR}/settings.json"',
    `API_KEY=${shellSingleQuoted(options.apiKey)}`,
    `BASE_URL=${shellSingleQuoted(options.baseUrl)}`,
    '',
    `echo "========== Claude Code quick setup (${platformLabel}) =========="`,
    'echo "Target directory: ${CLAUDE_DIR}"',
    'echo "API endpoint: ${BASE_URL}"',
    'echo',
    '',
    'mkdir -p "${CLAUDE_DIR}"',
    'STAMP="$(date +%Y%m%d_%H%M%S)"',
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
    claudeSettingsJson(options.baseUrl, options.apiKey).trimEnd(),
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
