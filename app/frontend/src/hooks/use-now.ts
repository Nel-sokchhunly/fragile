import {useEffect, useState} from 'react'

// Re-renders every `ms` so elapsed-time labels advance. Local clock only; not backend polling.
export function useNow(ms = 1000) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}
