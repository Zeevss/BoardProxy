import QRCode from 'qrcode'
import { useEffect, useState } from 'react'

/**
 * QR-код, отрисованный на месте.
 *
 * Дизайн предлагал `api.qrserver.com`, но это означало бы отправку keylink'а и
 * ссылки подписки — то есть готовых учётных данных — на сторонний сервис.
 * Библиотека весит около пятнадцати килобайт и снимает вопрос целиком.
 */
export function QrCode({ value, size = 200 }: { value: string; size?: number }) {
  const [src, setSrc] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    QRCode.toDataURL(value, {
      width: size * 2,
      margin: 1,
      errorCorrectionLevel: 'M',
      // Инверсия относительно тёмной темы: сканеры ждут тёмный код на светлом.
      color: { dark: '#09090b', light: '#fafafa' },
    })
      .then((url) => !cancelled && setSrc(url))
      .catch(() => !cancelled && setSrc(null))
    return () => {
      cancelled = true
    }
  }, [value, size])

  if (!src) return <div className="rounded-lg bg-line" style={{ width: size, height: size }} />

  return (
    <img
      src={src}
      alt=""
      width={size}
      height={size}
      className="rounded-lg"
      style={{ width: size, height: size }}
    />
  )
}
