// Wails runtime shim.
//
// At build time Wails injects two things onto `window`:
//   - `window.runtime` — EventsOn / EventsEmit / WindowMinimize etc.
//   - `window.go.<pkg>.<Struct>` — one property per exported method on each
//     bound Go struct. Our App struct lives in the `wailsapp` package, so the
//     path is `window.go.wailsapp.App`.
//
// These are populated inside the native webview. In a regular browser (or
// during `vite build` where window is not touched) they're absent, so every
// helper here guards with a runtime check.

export interface WailsResponse {
  status: number
  body: string
}

interface GoApp {
  Request(method: string, path: string, body: string): Promise<WailsResponse>
  PickDirectory?(title: string): Promise<string>
  RevealInFinder?(path: string): Promise<void>
}

interface WailsRuntime {
  EventsOn(kind: string, cb: (...args: any[]) => void): () => void
  EventsOff(kind: string, ...additional: string[]): void
  EventsEmit(kind: string, ...data: any[]): void
  WindowReload(): void
}

interface WailsWindow extends Window {
  go?: Record<string, Record<string, GoApp | undefined> | undefined>
  runtime?: WailsRuntime
}

function w(): WailsWindow {
  return window as WailsWindow
}

function findApp(): GoApp | undefined {
  const g = w().go
  if (!g) return undefined
  // Try common package names first, then fall back to scanning.
  const byPkg = g['wailsapp']?.App ?? g['main']?.App
  if (byPkg) return byPkg
  for (const pkg of Object.values(g)) {
    if (pkg?.App) return pkg.App
  }
  return undefined
}

export function isWails(): boolean {
  return !!findApp() && !!w().runtime
}

export function goApp(): GoApp {
  const a = findApp()
  if (!a) throw new Error('Wails bindings not available — launched outside the native app?')
  return a
}

export function runtime(): WailsRuntime {
  const r = w().runtime
  if (!r) throw new Error('Wails runtime not available — launched outside the native app?')
  return r
}

export async function pickDirectory(title = 'Select folder'): Promise<string | null> {
  const app = findApp()
  if (!app?.PickDirectory) return null
  const path = await app.PickDirectory(title)
  return path || null
}

export async function revealInFinder(path: string): Promise<boolean> {
  const app = findApp()
  if (!app?.RevealInFinder || !path) return false
  await app.RevealInFinder(path)
  return true
}
