import { describe, expect, it } from 'vitest'

import {
  buildCodexQuickSetupScript,
  buildCodexQuickSetupScripts,
  type CodexQuickSetupPlatform,
} from '@/utils/codexQuickSetup'

const options = {
  apiKey: 'sk-row-key-123',
  baseUrl: 'https://api.oreniva.com/',
}

describe('codex quick setup scripts', () => {
  it('builds one downloadable script for each supported platform', () => {
    const scripts = buildCodexQuickSetupScripts(options)

    expect(Object.keys(scripts).sort()).toEqual(['linux', 'macos', 'windows'])
    expect(scripts.windows.filename).toBe('codex-quick-setup-windows.bat')
    expect(scripts.macos.filename).toBe('codex-quick-setup-macos.sh')
    expect(scripts.linux.filename).toBe('codex-quick-setup-linux.sh')
  })

  it.each([
    ['windows', '%USERPROFILE%\\.codex'],
    ['macos', '${HOME}/.codex'],
    ['linux', '${HOME}/.codex'],
  ] as Array<[CodexQuickSetupPlatform, string]>)(
    'injects the selected row key and normalized base URL into the %s script',
    (platform, configDir) => {
      const script = buildCodexQuickSetupScript({ ...options, platform })

      expect(script.content).toContain('sk-row-key-123')
      expect(script.content).toContain('"OPENAI_API_KEY": "sk-row-key-123"')
      expect(script.content).toContain('base_url = "https://api.oreniva.com"')
      expect(script.content).toContain(configDir)
      expect(script.content).not.toContain('https://api.oreniva.com/')
    }
  )

  it('uses Windows batch semantics for the Windows download', () => {
    const script = buildCodexQuickSetupScript({ ...options, platform: 'windows' })

    expect(script.mimeType).toBe('application/x-bat')
    expect(script.content).toContain('@echo off')
    expect(script.content).toContain('set "API_KEY=sk-row-key-123"')
    expect(script.content).toContain('copy /Y "%CONFIG_FILE%" "%BACKUP_DIR%\\config.toml.bak"')
  })

  it.each(['macos', 'linux'] as CodexQuickSetupPlatform[])(
    'uses POSIX shell semantics for %s',
    (platform) => {
      const script = buildCodexQuickSetupScript({ ...options, platform })

      expect(script.mimeType).toBe('application/x-sh')
      expect(script.content).toContain('#!/usr/bin/env bash')
      expect(script.content).toContain("API_KEY='sk-row-key-123'")
      expect(script.content).toContain('cp "$CONFIG_FILE" "$BACKUP_DIR/config.toml.bak"')
    }
  )

  it('builds a Claude Code Windows settings script for anthropic groups', () => {
    const script = buildCodexQuickSetupScript({
      ...options,
      platform: 'windows',
      groupPlatform: 'anthropic',
    })

    expect(script.filename).toBe('claude-quick-setup-windows.bat')
    expect(script.mimeType).toBe('application/x-bat')
    expect(script.content).toContain('%USERPROFILE%\\.claude')
    expect(script.content).toContain('settings.json')
    expect(script.content).toContain('"ANTHROPIC_AUTH_TOKEN": "sk-row-key-123"')
    expect(script.content).toContain('"ANTHROPIC_BASE_URL": "https://api.oreniva.com"')
    expect(script.content).toContain('"ANTHROPIC_MODEL": "claude-sonnet-4-6"')
    expect(script.content).toContain('copy /Y "%SETTINGS_FILE%" "%BACKUP_DIR%\\settings.json.bak"')
    expect(script.content).not.toContain('%USERPROFILE%\\.codex')
    expect(script.content).not.toContain('OPENAI_API_KEY')
    expect(script.content).not.toContain('config.toml')
  })

  it.each([
    ['macos', '${HOME}/.claude'],
    ['linux', '${HOME}/.claude'],
  ] as Array<[CodexQuickSetupPlatform, string]>)(
    'builds a Claude Code %s settings script for anthropic groups',
    (platform, configDir) => {
      const script = buildCodexQuickSetupScript({
        ...options,
        platform,
        groupPlatform: 'anthropic',
      })

      expect(script.filename).toBe(`claude-quick-setup-${platform}.sh`)
      expect(script.mimeType).toBe('application/x-sh')
      expect(script.content).toContain(configDir)
      expect(script.content).toContain('SETTINGS_FILE="${CLAUDE_DIR}/settings.json"')
      expect(script.content).toContain('"ANTHROPIC_AUTH_TOKEN": "sk-row-key-123"')
      expect(script.content).toContain('"ANTHROPIC_BASE_URL": "https://api.oreniva.com"')
      expect(script.content).toContain('cp "$SETTINGS_FILE" "$BACKUP_DIR/settings.json.bak"')
      expect(script.content).not.toContain('${HOME}/.codex')
      expect(script.content).not.toContain('OPENAI_API_KEY')
      expect(script.content).not.toContain('config.toml')
    }
  )
})
