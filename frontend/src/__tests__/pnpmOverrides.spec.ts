import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// `pnpm test` runs with cwd at frontend/. import.meta.url is not a file URL under Vitest.
const frontendRoot = process.cwd()
const repoRoot = resolve(frontendRoot, '..')

// Pins the frozen lockfile already enforces. pnpm 9 reads package.json
// pnpm.overrides; pnpm 11 reads pnpm-workspace.yaml and ignores the package.json
// block. Both have to match the lockfile or `pnpm install --frozen-lockfile` fails.
const requiredPins: Record<string, string> = {
  'js-cookie': '3.0.7',
  'form-data@<4.0.6': '>=4.0.6',
  postcss: '8.5.22',
  'dompurify@<3.4.14': '>=3.4.14',
  'source-map-js@<1.2.2': '>=1.2.2'
}

function unquote(raw: string): string {
  const trimmed = raw.trim()
  if (
    (trimmed.startsWith("'") && trimmed.endsWith("'")) ||
    (trimmed.startsWith('"') && trimmed.endsWith('"'))
  ) {
    return trimmed.slice(1, -1)
  }
  return trimmed
}

function parseOverridesBlock(source: string): Record<string, string> {
  const lines = source.split(/\r?\n/)
  const start = lines.findIndex((line) => line === 'overrides:')
  if (start < 0) {
    throw new Error('overrides block not found')
  }

  const overrides: Record<string, string> = {}
  for (const line of lines.slice(start + 1)) {
    if (line.trim() === '') {
      continue
    }
    if (!line.startsWith('  ')) {
      break
    }
    const trimmed = line.trim()
    if (trimmed.startsWith('#')) {
      continue
    }
    const sep = trimmed.indexOf(':')
    if (sep < 0) {
      throw new Error(`override line has no colon: ${line}`)
    }
    const key = unquote(trimmed.slice(0, sep))
    const value = unquote(trimmed.slice(sep + 1))
    if (value === '') {
      throw new Error(`override value missing: ${line}`)
    }
    overrides[key] = value
  }

  if (Object.keys(overrides).length === 0) {
    throw new Error('overrides block was empty')
  }
  return overrides
}

describe('frontend pnpm overrides', () => {
  const packageJson = JSON.parse(readFileSync(resolve(frontendRoot, 'package.json'), 'utf8')) as {
    name?: string
    pnpm?: { overrides?: Record<string, string> }
  }
  const packageOverrides = packageJson.pnpm?.overrides
  const workspaceOverrides = parseOverridesBlock(readFileSync(resolve(frontendRoot, 'pnpm-workspace.yaml'), 'utf8'))
  const lockfileOverrides = parseOverridesBlock(readFileSync(resolve(frontendRoot, 'pnpm-lock.yaml'), 'utf8'))

  it('reads the frontend package from the test cwd', () => {
    expect(packageJson.name).toBe('sub2api-frontend')
  })

  it('keeps the pnpm 9 package.json overrides equal to the lockfile', () => {
    expect(packageOverrides).toEqual(lockfileOverrides)
  })

  it('keeps the pnpm 11 workspace overrides equal to the lockfile', () => {
    expect(workspaceOverrides).toEqual(lockfileOverrides)
  })

  it('keeps the security pins that the lockfile already records', () => {
    for (const [key, value] of Object.entries(requiredPins)) {
      expect(lockfileOverrides[key]).toBe(value)
    }
  })

  it('installs frontend dependencies with a frozen lockfile during deploy', () => {
    const script = readFileSync(resolve(repoRoot, 'deploy/deploy-zero-downtime.sh'), 'utf8')
    const installLines = script.split(/\r?\n/).filter((line) => line.includes('pnpm install'))

    expect(installLines).toEqual(['        pnpm install --frozen-lockfile'])
  })
})
