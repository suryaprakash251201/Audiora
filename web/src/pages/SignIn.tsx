import { useState } from 'react'
import { useAuth } from '../lib/auth'
import { Logo } from '../components/Logo'

/**
 * The sign-in screen. On a fresh server there are no accounts, so the same
 * form doubles as first-run setup and creates the administrator.
 */
export function SignIn() {
  const { login, register, needsSetup } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [mode, setMode] = useState<'signin' | 'signup'>(needsSetup ? 'signup' : 'signin')

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      if (mode === 'signin') {
        await login(email.trim(), password)
      } else {
        await register(email.trim(), password, name.trim())
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  const isSetup = mode === 'signup'

  return (
    <div className="relative grid min-h-full place-items-center overflow-hidden px-5 py-10">
      <div className="ambient" aria-hidden="true" />

      <div className="animate-rise relative w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <Logo className="mb-5 h-14 w-14" />
          <h1 className="text-3xl font-bold tracking-tight text-ink-100">Audiora</h1>
          <p className="mt-1.5 text-sm text-ink-400">
            {isSetup ? 'Create the first administrator account' : 'Your music, self-hosted'}
          </p>
        </div>

        <form onSubmit={onSubmit} className="glass rounded-[var(--radius-glass)] p-6">
          {isSetup && (
            <label className="mb-4 block">
              <span className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-ink-400">
                Name
              </span>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoComplete="name"
                className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5
                  text-ink-100 outline-none transition
                  placeholder:text-ink-600 focus:border-[var(--accent-live)] focus:bg-white/8"
                placeholder="Your name"
              />
            </label>
          )}

          <label className="mb-4 block">
            <span className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-ink-400">
              Email
            </span>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="username"
              className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5
                text-ink-100 outline-none transition
                placeholder:text-ink-600 focus:border-[var(--accent-live)] focus:bg-white/8"
              placeholder="you@example.com"
            />
          </label>

          <label className="mb-5 block">
            <span className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-ink-400">
              Password
            </span>
            <input
              type="password"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={isSetup ? 'new-password' : 'current-password'}
              className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5
                text-ink-100 outline-none transition
                placeholder:text-ink-600 focus:border-[var(--accent-live)] focus:bg-white/8"
              placeholder={isSetup ? 'At least 8 characters' : '••••••••'}
            />
          </label>

          {error && (
            <p
              role="alert"
              className="mb-4 rounded-lg border border-red-400/25 bg-red-500/10 px-3 py-2 text-sm text-red-200"
            >
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={busy}
            className="pressable w-full rounded-xl bg-[var(--accent-live)] py-3 font-semibold
              text-ink-950 shadow-lg transition
              hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {busy ? 'Just a moment…' : isSetup ? 'Create account' : 'Sign in'}
          </button>

          <button
            type="button"
            onClick={() => {
              setMode(isSetup ? 'signin' : 'signup')
              setError(null)
            }}
            className="mt-4 w-full text-center text-xs text-ink-400 transition hover:text-ink-200"
          >
            {isSetup ? 'Already have an account? Sign in' : 'Setting up a new server? Create the first account'}
          </button>
        </form>

        <p className="mt-6 text-center text-[11px] leading-relaxed text-ink-600">
          Your music never leaves this server.
        </p>
      </div>
    </div>
  )
}
