import QRCode from 'qrcode'
import { useMemo } from 'react'
import { useT } from '@/app/language'
import { CopyButton } from '@/components/ui/copy'
import { Modal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'

/**
 * QR-код, отрисованный на месте.
 *
 * Дизайн предлагал `api.qrserver.com`, но это означало бы отправку keylink'а и
 * ссылки подписки — то есть готовых учётных данных — на сторонний сервис.
 * Библиотека весит около пятнадцати килобайт и снимает вопрос целиком.
 *
 * Рисуем не растр из `toDataURL`, а свой SVG по матрице модулей: так код
 * остаётся чётким на любом размере и на любом экране.
 *
 * Три поисковых квадрата рисуются отдельными рамками со скруглёнными углами —
 * единственное отступление от канонической картинки, и стоит оно девяти
 * модулей по углам этих рамок из четырёх с лишним тысяч. Распознавание держится
 * не на них: сканер ищет соотношение 1:1:3:1:1 вдоль средних линий квадрата, а
 * средних линий скругление не задевает — сверено на отрисованном коде.
 */
export function QrCode({ value, size = 200, className }: { value: string; size?: number; className?: string }) {
  // Построение матрицы синхронное и дешёвое, так что считаем его прямо при
  // отрисовке: эффект здесь дал бы лишний кадр с пустым местом вместо кода.
  // Уровень Q переживает и печать, и палец на экране: ссылки подписки длинные,
  // но до предела версии им далеко.
  const { bits, count } = useMemo(() => {
    const created = QRCode.create(value, { errorCorrectionLevel: 'Q' })
    return { bits: created.modules.data as Uint8Array, count: created.modules.size }
  }, [value])

  const quiet = 2
  const span = count + quiet * 2
  const on = (x: number, y: number) =>
    x >= 0 && y >= 0 && x < count && y < count && bits[y * count + x] === 1

  // Поисковые квадраты рисуются рамками, поэтому их модули из общей сетки
  // исключаются — иначе поверх рамки легла бы россыпь точек.
  const finders = [
    [0, 0],
    [count - 7, 0],
    [0, count - 7],
  ]
  const inFinder = (x: number, y: number) =>
    finders.some(([fx, fy]) => x >= fx && x < fx + 7 && y >= fy && y < fy + 7)

  // Модули рисуются заливкой, а не обводкой.
  //
  // Сначала здесь стояли точки — подпути из одного `M` со скруглённым концом.
  // Такой подпуть не обводится вовсе: обводить нечего, и код выходил пустым,
  // с одними рамками по углам. Сплошные квадраты к тому же смыкаются между
  // собой, а это именно то, что ищет сканер.
  const modules: string[] = []
  for (let y = 0; y < count; y += 1) {
    for (let x = 0; x < count; x += 1) {
      if (on(x, y) && !inFinder(x, y)) modules.push(`M${x + quiet} ${y + quiet}h1v1h-1z`)
    }
  }

  return (
    <svg
      viewBox={`0 0 ${span} ${span}`}
      width={size}
      height={size}
      role="img"
      aria-label="QR"
      className={cn('block', className)}
      style={{ width: size, height: size }}
      shapeRendering="geometricPrecision"
    >
      <rect width={span} height={span} rx={1.5} fill="#fafafa" />
      <path d={modules.join('')} fill="#09090b" />
      {finders.map(([fx, fy]) => (
        <g key={`${fx}-${fy}`}>
          <rect
            x={fx + quiet + 0.5}
            y={fy + quiet + 0.5}
            width={6}
            height={6}
            rx={1.4}
            fill="none"
            stroke="#09090b"
            strokeWidth={1}
          />
          <rect
            x={fx + quiet + 2}
            y={fy + quiet + 2}
            width={3}
            height={3}
            rx={1}
            fill="#09090b"
          />
        </g>
      ))}
    </svg>
  )
}

/**
 * QR по центру экрана.
 *
 * Раньше код раскрывался прямо в строке списка: он распирал строку, уезжал под
 * край выезжающей панели и его приходилось ловить прокруткой. Телефон в руке
 * наводят на середину экрана, а не на угол.
 */
export function QrDialog({
  open,
  title,
  value,
  onClose,
}: {
  open: boolean
  title: string
  value: string | null
  onClose: () => void
}) {
  const t = useT()
  return (
    <Modal
      open={open && value !== null}
      onOpenChange={(next) => !next && onClose()}
      title={title}
      className="max-w-[400px]"
    >
      {value ? (
        <div className="flex flex-col items-center gap-4">
          <div className="rounded-2xl bg-[#fafafa] p-4 shadow-lg">
            <QrCode value={value} size={244} />
          </div>
          <p className="w-full rounded-lg border border-line bg-raised px-3 py-2.5 text-center font-mono text-[11.5px] break-all text-soft">
            {value}
          </p>
          <CopyButton variant="raised" value={value} label={title}>
            {t.copy}
          </CopyButton>
        </div>
      ) : null}
    </Modal>
  )
}
