/**
 * Знаки платёжных систем — UnitPay требует их на сайте рядом с оплатой и в
 * футере. Простые встроенные SVG: без внешних запросов и одинаково на светлом
 * и тёмном фоне (у каждого знака своя белая плашка).
 */
export function PaymentLogos({ className = '' }: { className?: string }) {
  return (
    <div className={`flex items-center gap-2 ${className}`} aria-label="Принимаем к оплате Visa и Mastercard">
      <svg viewBox="0 0 48 30" className="h-6 w-auto" role="img" aria-label="Visa">
        <rect width="48" height="30" rx="4" fill="#fff" stroke="#E5E7EB" />
        <text x="24" y="20" textAnchor="middle" fontFamily="Arial, Helvetica, sans-serif" fontSize="13" fontStyle="italic" fontWeight="800" fill="#1A1F71">
          VISA
        </text>
      </svg>
      <svg viewBox="0 0 48 30" className="h-6 w-auto" role="img" aria-label="Mastercard">
        <rect width="48" height="30" rx="4" fill="#fff" stroke="#E5E7EB" />
        <circle cx="19.5" cy="15" r="8" fill="#EB001B" />
        <circle cx="28.5" cy="15" r="8" fill="#F79E1B" />
        <path d="M24 8.4a8 8 0 0 1 0 13.2a8 8 0 0 1 0-13.2Z" fill="#FF5F00" />
      </svg>
    </div>
  )
}
