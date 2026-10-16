const TOKEN_KEY = 'admin_token'
const USER_KEY = 'admin_user'

export type StoredUser = {
  userId: string
  username: string
  role: 'admin' | 'user'
  permissions: string[]
}

/** Legacy admin token storage (unused in MVP; kept for PublicRoot compatibility). */
export function tokenStorage() {
  return {
    get: () => window.localStorage.getItem(TOKEN_KEY),
    set: (t: string) => window.localStorage.setItem(TOKEN_KEY, t),
    clear: () => {
      window.localStorage.removeItem(TOKEN_KEY)
      window.localStorage.removeItem(USER_KEY)
    },
    getUser: (): StoredUser | null => {
      try {
        const raw = window.localStorage.getItem(USER_KEY)
        return raw ? JSON.parse(raw) : null
      } catch {
        return null
      }
    },
    setUser: (u: StoredUser) => window.localStorage.setItem(USER_KEY, JSON.stringify(u)),
  }
}
