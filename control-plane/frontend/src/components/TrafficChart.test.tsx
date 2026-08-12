import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { TrafficChart } from './TrafficChart'

describe('TrafficChart', () => {
  it('switches between physically observed interface traffic and user payload traffic', () => {
    const interfacePoints = [{ bucket: '2026-08-12T10:00:00Z', subject: 'eth0', rxBytes: 10, txBytes: 20 }]
    render(<TrafficChart interfacePoints={interfacePoints} userPoints={[]} />)

    expect(screen.queryByText('No traffic samples')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'User payload' }))
    expect(screen.getByText('No traffic samples')).toBeInTheDocument()
  })
})
