import { afterEach, describe, expect, it } from 'vitest'
import { existsSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { buildCodexQuickSetupScript, buildCodexQuickSetupScripts, type CodexQuickSetupPlatform } from '@/utils/codexQuickSetup'
import { CLIENT_SETUP_DEFAULT_MODELS } from '@/utils/clientSetupModels'
import type { GroupPlatform } from '@/types'

const options = { apiKey: 'sk-row-key-123', baseUrl: 'https://api.example.com/' }

function scriptFiles(platform: CodexQuickSetupPlatform, content: string): Record<string, string> {
  if (platform === 'windows') {
    const block = content.match(/\$files = \[ordered\]@\{([\s\S]*?)\r\n\}\r\n\$stage =/)?.[1]
    if (!block) throw new Error('Windows script must expose readable configuration files')
    return Object.fromEntries([...block.matchAll(/ {2}'([^']+)' = '((?:[^']|'')*)'/g)]
      .map((match) => [match[1], match[2].replaceAll("''", "'")]))
  }
  const files: Record<string, string> = {}
  for (const [file, marker] of [['config.toml', 'CODEX_CONFIG_TOML'], ['auth.json', 'CODEX_AUTH_JSON'], ['settings.json', 'CLAUDE_SETTINGS_JSON']]) {
    const contentMatch = content.match(new RegExp(`<<'${marker}'\\n([\\s\\S]*?)\\n${marker}`))
    if (contentMatch) files[file] = contentMatch[1]
  }
  return files
}

const fixtures: string[] = []
afterEach(() => {
  for (const fixture of fixtures.splice(0)) {
    if (dirname(resolve(fixture)) !== resolve(tmpdir())) throw new Error('fixture cleanup escaped temporary directory')
    rmSync(fixture, { recursive: true, force: true })
  }
})

describe('client quick setup scripts', () => {
  it('builds Windows, macOS and Linux scripts', () => {
    const scripts = buildCodexQuickSetupScripts(options)
    expect(Object.keys(scripts).sort()).toEqual(['linux', 'macos', 'windows'])
    expect(scripts.windows.filename).toBe('codex-quick-setup-windows.bat')
    expect(scripts.macos.filename).toBe('codex-quick-setup-macos.sh')
    expect(scripts.linux.filename).toBe('codex-quick-setup-linux.sh')
  })

  it.each(['windows', 'macos', 'linux'] as const)('writes a usable API-key Codex provider for %s', platform => {
    const script = buildCodexQuickSetupScript({ ...options, platform })
    const files = scriptFiles(platform, script.content)
    expect(files['config.toml']).toContain('model = "gpt-6.1-sol"')
    expect(files['config.toml']).toContain('base_url = "https://api.example.com/v1"')
    expect(files['config.toml']).toContain('requires_openai_auth = false')
    expect(files['config.toml']).toContain('experimental_bearer_token = "sk-row-key-123"')
    expect(files['config.toml']).not.toContain('model_reasoning_effort')
    expect(files['config.toml']).not.toContain('model_context_window')
    expect(JSON.parse(files['auth.json']).OPENAI_API_KEY).toBe(options.apiKey)
    if (platform === 'windows') {
      expect(script.content).toContain('model = "gpt-6.1-sol"')
      expect(script.content).not.toContain('-EncodedCommand')
      expect(script.content.split('\r\n').every(line => line.length < 8191)).toBe(true)
    }
  })

  it.each(Object.entries(CLIENT_SETUP_DEFAULT_MODELS).filter(([platform]) => platform !== 'typesafe'))('uses a platform-specific default for %s', (groupPlatform, model) => {
    const script = buildCodexQuickSetupScript({ ...options, platform: 'linux', groupPlatform: groupPlatform as GroupPlatform })
    const files = scriptFiles('linux', script.content)
    if (groupPlatform === 'anthropic') expect(JSON.parse(files['settings.json']).model).toBe(model)
    else expect(files['config.toml']).toContain(`model = "${model}"`)
  })

  it.each(['windows', 'macos', 'linux'] as const)('keeps Claude model and role defaults consistent on %s', platform => {
    const script = buildCodexQuickSetupScript({ ...options, baseUrl: 'https://api.example.com/v1/', platform, groupPlatform: 'anthropic' })
    const settings = JSON.parse(scriptFiles(platform, script.content)['settings.json'])
    expect(settings.model).toBe('claude-sonnet-5-5')
    expect(settings.env.ANTHROPIC_MODEL).toBe(settings.model)
    expect(settings.env.ANTHROPIC_DEFAULT_OPUS_MODEL).toBe('claude-opus-5-5')
    expect(settings.env.ANTHROPIC_BASE_URL).toBe('https://api.example.com')
    expect(settings).not.toHaveProperty('permissions')
    expect(settings).not.toHaveProperty('skipDangerousModePermissionPrompt')
  })

  it('uses the actual group model and refuses an empty catalog or unsupported client', () => {
    const script = buildCodexQuickSetupScript({ ...options, platform: 'linux', groupPlatform: 'composite', availableModels: ['gpt-6-sol'] })
    expect(scriptFiles('linux', script.content)['config.toml']).toContain('model = "gpt-6-sol"')
    expect(() => buildCodexQuickSetupScript({ ...options, platform: 'linux', availableModels: [] })).toThrow('No usable text model')
    expect(() => buildCodexQuickSetupScript({ ...options, platform: 'linux', groupPlatform: 'typesafe' })).toThrow('/v1/systemone')
  })

  it.each(['windows', 'linux'] as const)('executes %s config imports and backups only inside a temporary fixture', (platform, context) => {
    const bash = process.platform === 'win32' ? ['C:/Program Files/Git/bin/bash.exe', 'D:/Program Files/Git/bin/bash.exe'].find(existsSync) || '' : '/bin/bash'
    if (platform === 'windows' && process.platform !== 'win32') return context.skip()
    if (platform === 'linux' && !existsSync(bash)) return context.skip()
    const fixture = mkdtempSync(join(tmpdir(), platform === 'windows' ? "sub2api-quick-setup-中文 ' & (!)% " : 'sub2api-quick-setup-'))
    fixtures.push(fixture)
    for (const groupPlatform of ['openai', 'anthropic'] as const) {
      const apiKey = 'sk-%&$!"\\quoted-\'中文-$(throw)-`\n-key' + (platform === 'windows' ? 'x'.repeat(9000) : '')
      const script = buildCodexQuickSetupScript({ ...options, apiKey, platform, groupPlatform })
      const directory = join(fixture, groupPlatform === 'anthropic' ? '.claude' : '.codex')
      mkdirSync(directory, { recursive: true })
      const filename = groupPlatform === 'anthropic' ? 'settings.json' : 'config.toml'
      const original = groupPlatform === 'anthropic' ? '{"permissions":{"defaultMode":"default"}}' : 'model = "previous"'
      writeFileSync(join(directory, filename), original)
      if (groupPlatform === 'openai') writeFileSync(join(directory, 'auth.json'), '{"OPENAI_API_KEY":"previous-key"}')
      const expected = scriptFiles(platform, script.content)
      let result
      if (platform === 'windows') {
        const safeRoot = "'" + fixture.replace(/'/g, "''") + "'"
        const scriptName = `${groupPlatform}-setup.bat`
        writeFileSync(join(fixture, scriptName), script.content.replace('$env:USERPROFILE', safeRoot), 'utf8')
        result = spawnSync('cmd.exe', ['/d', '/c', scriptName], { cwd: fixture, input: '\r\n', encoding: 'utf8', timeout: 15000 })
      } else {
        const safeRoot = fixture.replace(/\\/g, '/')
        const content = script.content.replaceAll('$' + '{HOME}', safeRoot)
        const scriptPath = join(fixture, `${groupPlatform}.sh`)
        writeFileSync(scriptPath, content)
        result = spawnSync(bash, [scriptPath], { encoding: 'utf8', timeout: 15000 })
      }
      expect(result.status, result.stderr).toBe(0)
      for (const [name, content] of Object.entries(expected)) expect(readFileSync(join(directory, name), 'utf8').trim()).toBe(content.trim())
      const backup = readdirSync(join(directory, 'backup'))
      expect(backup).toHaveLength(1)
      expect(readFileSync(join(directory, 'backup', backup[0], `${filename}.bak`), 'utf8')).toBe(original)
      if (groupPlatform === 'openai') expect(readFileSync(join(directory, 'backup', backup[0], 'auth.json.bak'), 'utf8')).toBe('{"OPENAI_API_KEY":"previous-key"}')
    }
  }, 30000)

  it('keeps the original files when Windows backup fails and returns a readable error', (context) => {
    if (process.platform !== 'win32') return context.skip()
    const fixture = mkdtempSync(join(tmpdir(), 'sub2api-quick-setup-failure-'))
    fixtures.push(fixture)
    const directory = join(fixture, '.codex')
    mkdirSync(directory)
    const originals = { 'config.toml': 'model = "previous"', 'auth.json': '{"OPENAI_API_KEY":"previous-key"}' }
    for (const [name, content] of Object.entries(originals)) writeFileSync(join(directory, name), content)
    writeFileSync(join(directory, 'backup'), 'block backup directory creation')
    const script = buildCodexQuickSetupScript({ ...options, platform: 'windows' })
    writeFileSync(join(fixture, 'setup.bat'), script.content.replace('$env:USERPROFILE', "'" + fixture.replace(/'/g, "''") + "'"), 'utf8')
    const result = spawnSync('cmd.exe', ['/d', '/c', 'setup.bat'], { cwd: fixture, input: '\r\n', encoding: 'utf8', timeout: 15000 })
    expect(result.status).not.toBe(0)
    expect(result.stderr).toContain('back up existing configuration files')
    expect(result.stdout + result.stderr).not.toContain(options.apiKey)
    for (const [name, content] of Object.entries(originals)) expect(readFileSync(join(directory, name), 'utf8')).toBe(content)
  })
})
