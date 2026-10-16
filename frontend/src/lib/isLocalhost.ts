// Возвращает true, если страница открыта в локальной разработке.
// Используется, чтобы не грузить аналитику и не слать события с localhost.
export function isLocalhost(): boolean {
  if (typeof window === 'undefined') return false
  const h = window.location.hostname
  return h === 'localhost' || h === '127.0.0.1' || h === '::1' || h.endsWith('.local')
}
