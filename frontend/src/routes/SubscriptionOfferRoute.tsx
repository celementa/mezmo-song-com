import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

// Условия подписки — страница, на которую ссылается чекбокс согласия при
// оформлении. Нужна ЮKassa для согласования рекуррентных платежей: цена,
// период, порядок списаний и отмены описаны явно.
export function SubscriptionOfferRoute() {
  return (
    <div className="min-h-screen bg-white">
      <div className="mx-auto max-w-3xl px-4 py-12 sm:py-16">
        <Link to="/subscribe" className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-600 transition-colors mb-8">
          <ArrowLeft className="h-4 w-4" />
          К оформлению подписки
        </Link>

        <h1 className="text-3xl font-bold text-gray-900 mb-2">Условия подписки</h1>
        <p className="text-sm text-gray-400 mb-10">Последнее обновление: 27 августа 2026 г.</p>

        <div className="prose prose-gray max-w-none space-y-8 text-gray-600 text-[15px] leading-relaxed">
          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-3">1. Что такое подписка</h2>
            <p>
              Подписка даёт доступ к созданию персональных песен без поштучной оплаты: создание новых песен и их
              доработки в течение оплаченного периода, а также личный кабинет со всеми созданными песнями. Настоящие
              условия дополняют{' '}
              <Link to="/terms" className="text-rose-600 hover:underline">Пользовательское соглашение</Link> и{' '}
              <Link to="/privacy" className="text-rose-600 hover:underline">Политику конфиденциальности</Link>.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-3">2. Стоимость и порядок оплаты</h2>
            <ul className="list-disc pl-5 space-y-2 mt-3">
              <li>Стоимость подписки — 990 (девятьсот девяносто) рублей за один расчётный период (календарный месяц).</li>
              <li>
                Первый платёж проходит при оформлении подписки; при оплате банковская карта сохраняется для
                последующих автоматических списаний (рекуррентные платежи через сервис ЮKassa).
              </li>
              <li>
                Далее оплата списывается автоматически один раз в месяц в дату, соответствующую дате оформления.
                За 3 дня до каждого списания мы отправляем напоминание на email подписки.
              </li>
              <li>
                Если списание не прошло (например, на карте недостаточно средств), мы повторяем попытку примерно раз
                в 7 дней в течение 30 дней, после чего подписка прекращается автоматически.
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-3">3. Добросовестное использование</h2>
            <p>
              Подписка не ограничивает число песен в месяц, но действует суточный лимит — не более 5 новых песен в
              сутки. Лимит защищает сервис от автоматизированных злоупотреблений и перепродажи; для личного
              использования его достаточно с запасом. Доработки созданных песен не ограничиваются.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-3">4. Отмена подписки</h2>
            <ul className="list-disc pl-5 space-y-2 mt-3">
              <li>
                Отменить подписку можно в любой момент: кнопкой в{' '}
                <Link to="/account" className="text-rose-600 hover:underline">личном кабинете</Link>, на{' '}
                <Link to="/subscription/cancel" className="text-rose-600 hover:underline">странице отмены</Link> или
                по ссылке из любого письма о подписке.
              </li>
              <li>После отмены автоматические списания прекращаются, привязка карты удаляется.</li>
              <li>
                Доступ к кабинету и создание песен сохраняются до конца уже оплаченного периода. Созданные песни
                остаются доступны по ссылкам из писем и после окончания подписки.
              </li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-3">5. Возвраты</h2>
            <p>
              Оплата за начавшийся расчётный период не возвращается, за исключением случаев, предусмотренных
              законодательством РФ. По вопросам возвратов и работы подписки пишите через{' '}
              <Link to="/contacts" className="text-rose-600 hover:underline">страницу контактов</Link>.
            </p>
          </section>
        </div>
      </div>
    </div>
  )
}
