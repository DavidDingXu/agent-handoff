/** Load the installed Host's actual lazy-CJS renderer without booting a profile or a standalone app. */
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { runInThisContext } from 'node:vm'
import * as React from 'react'
import * as ReactDOM from 'react-dom'
import * as ReactDOMClient from 'react-dom/client'
import * as JsxRuntime from 'react/jsx-runtime'
import * as Cordis from '@deepseek-ai/cordis'
import * as Slots from '@deepseek-ai/dsh-client-ui-slots'
import type { SlotRegistry as Registry } from '@deepseek-ai/dsh-client-ui-renderer/client'
const required = new Map<string, unknown>([
  ['react', React], ['react-dom', ReactDOM], ['react-dom/client', ReactDOMClient], ['react/jsx-runtime', JsxRuntime],
  ['@deepseek-ai/cordis', Cordis], ['@deepseek-ai/dsh-client-ui-slots', Slots],
])
let factory: ((require: (name: string) => unknown) => { SlotRegistry: typeof Registry }) | undefined
const source = await readFile(createRequire(import.meta.url).resolve('@deepseek-ai/dsh-client-ui-renderer/client'), 'utf8')
const register = runInThisContext(`(function(window) { ${source}\n})`, { filename: 'installed-dsh-renderer-client.js' }) as (window: object) => void
register({ __ModuleLoader__: { load: (entry: { factory: typeof factory }) => { factory = entry.factory } } })
if (!factory) throw new Error('The installed DSH renderer registered no native factory')
export const { SlotRegistry } = factory(name => {
  if (!required.has(name)) throw new Error(`Unexpected native renderer dependency: ${name}`)
  return required.get(name)
})
