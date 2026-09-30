import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  api,
  clearSession,
  loadSession,
  saveSession,
  setAuthLostHandler,
  type User,
} from './api'

interface AuthState {
  user: User | null
  loading: boolean
  /** True when the server has no accounts yet, so the app should offer sign-up. */
  needsSetup: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, password: string, name: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [needsSetup, setNeedsSetup] = useState(false)

  // On boot, try to restore the session from stored tokens.
  useEffect(() => {
    let cancelled = false

    async function restore() {
      if (!loadSession()) {
        setLoading(false)
        return
      }
      try {
        const me = await api.me()
        if (!cancelled) setUser(me)
      } catch {
        // The refresh in the api client already tried; if it still failed the
        // stored tokens are gone.
        clearSession()
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void restore()
    return () => {
      cancelled = true
    }
  }, [])

  // A 401 anywhere in the app drops us back to the sign-in screen.
  useEffect(() => {
    setAuthLostHandler(() => {
      setUser(null)
    })
  }, [])

  // A 403 on the first sign-in attempt means the server already has accounts,
  // so the sign-up form should be hidden.
  const login = useCallback(async (email: string, password: string) => {
    const result = await api.login(email, password)
    saveSession({
      accessToken: result.accessToken,
      refreshToken: result.refreshToken,
      expiresAt: result.expiresAt,
    })
    setNeedsSetup(false)
    setUser(result.user)
  }, [])

  const register = useCallback(async (email: string, password: string, name: string) => {
    try {
      const result = await api.register(email, password, name)
      saveSession({
        accessToken: result.accessToken,
        refreshToken: result.refreshToken,
        expiresAt: result.expiresAt,
      })
      setUser(result.user)
    } catch (err) {
      // "Registration closed" means accounts already exist, which is the
      // signal to switch this screen from sign-up to sign-in.
      const message = err instanceof Error ? err.message : ''
      if (message.includes('already has accounts')) {
        setNeedsSetup(false)
      }
      throw err
    }
  }, [])

  const logout = useCallback(async () => {
    const session = loadSession()
    if (session?.refreshToken) {
      try {
        await api.logout(session.refreshToken)
      } catch {
        // Signing out locally matters more than the server acknowledging it.
      }
    }
    clearSession()
    setUser(null)
  }, [])

  const value = useMemo(
    () => ({ user, loading, needsSetup, login, register, logout }),
    [user, loading, needsSetup, login, register, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside an AuthProvider')
  return ctx
}
