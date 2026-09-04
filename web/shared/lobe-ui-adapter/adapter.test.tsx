/*
Copyright (C) 2026 GrowthOS fork contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

import { describe, expect, test } from 'bun:test'
import { renderToStaticMarkup } from 'react-dom/server'

import { Center, Flexbox, Icon, Tag } from './index'
import { ProviderIcon } from './icons'

describe('private Lobe icon UI adapter', () => {
  test('Center and Flexbox retain the layout contract used by icon features', () => {
    const center = renderToStaticMarkup(
      <Center flex='none' style={{ color: 'red' }}>
        center
      </Center>
    )
    const flexbox = renderToStaticMarkup(
      <Flexbox align='center' gap={8} height={24} horizontal width='fit-content'>
        row
      </Flexbox>
    )

    expect(center).toContain('display:flex')
    expect(center).toContain('align-items:center')
    expect(center).toContain('justify-content:center')
    expect(center).toContain('flex:none')
    expect(center).toContain('color:red')
    expect(flexbox).toContain('flex-direction:row')
    expect(flexbox).toContain('align-items:center')
    expect(flexbox).toContain('gap:8px')
    expect(flexbox).toContain('height:24px')
    expect(flexbox).toContain('width:fit-content')
  })

  test('Icon forwards visual props to the supplied SVG component', () => {
    const Glyph = ({ color, size }: { color?: string; size?: number }) => (
      <svg data-glyph='true' height={size} stroke={color} width={size} />
    )

    const markup = renderToStaticMarkup(<Icon color='#123456' icon={Glyph} size={18} />)

    expect(markup).toContain('data-glyph="true"')
    expect(markup).toContain('height="18"')
    expect(markup).toContain('width="18"')
    expect(markup).toContain('stroke="#123456"')
  })

  test('Tag and ProviderIcon expose bounded accessible fallback markup', () => {
    const tag = renderToStaticMarkup(
      <Tag icon={<ProviderIcon data-testid='provider' size={12} />}>model</Tag>
    )
    const icon = renderToStaticMarkup(<ProviderIcon aria-label='Provider' size={20} />)

    expect(tag).toContain('model')
    expect(tag).toContain('data-testid="provider"')
    expect(tag).toContain('display:inline-flex')
    expect(icon).toContain('aria-label="Provider"')
    expect(icon).toContain('height="20"')
    expect(icon).toContain('width="20"')
    expect(icon.match(/<rect/g)).toHaveLength(3)
  })
})
