/**
 * Get or create a stable session ID for the current browser tab.
 * Uses sessionStorage so it persists across page reloads but not across tabs.
 */
export function getSessionId(): string {
  let sid = sessionStorage.getItem('_sid')
  if (!sid) {
    sid = crypto.randomUUID()
    sessionStorage.setItem('_sid', sid)
  }
  return sid
}
