/*
Copyright (C) 2026 GrowthOS fork contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

import { forwardRef, type SVGAttributes } from 'react'

export interface ProviderIconProps extends SVGAttributes<SVGSVGElement> {
  size?: number | string
}

export const ProviderIcon = forwardRef<SVGSVGElement, ProviderIconProps>(
  function ProviderIcon({ size = 24, ...props }, ref) {
    return (
      <svg
        {...props}
        fill='none'
        height={size}
        ref={ref}
        viewBox='0 0 24 24'
        width={size}
        xmlns='http://www.w3.org/2000/svg'
      >
        <rect height='6' rx='1.5' stroke='currentColor' strokeWidth='1.75' width='6' x='2' y='9' />
        <rect height='6' rx='1.5' stroke='currentColor' strokeWidth='1.75' width='6' x='16' y='3' />
        <rect height='6' rx='1.5' stroke='currentColor' strokeWidth='1.75' width='6' x='16' y='15' />
        <path d='M8 12h4m0 0 4-6m-4 6 4 6' stroke='currentColor' strokeLinecap='round' strokeWidth='1.75' />
      </svg>
    )
  }
)
