import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// vitest работает без globals, поэтому авто-cleanup RTL не подключается сам:
// без этого DOM предыдущего теста остаётся в документе и ломает запросы следующего.
afterEach(cleanup)
