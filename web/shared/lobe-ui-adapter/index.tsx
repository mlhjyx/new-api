/*
Copyright (C) 2026 GrowthOS fork contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

import {
  forwardRef,
  isValidElement,
  type ComponentType,
  type CSSProperties,
  type ElementType,
  type HTMLAttributes,
  type ReactElement,
  type ReactNode,
  type Ref,
} from 'react'

type CSSDimension = CSSProperties['width']

export interface FlexboxProps extends HTMLAttributes<HTMLElement> {
  align?: CSSProperties['alignItems']
  allowShrink?: boolean
  as?: ElementType
  direction?: 'horizontal' | 'horizontal-reverse' | 'vertical' | 'vertical-reverse'
  distribution?: CSSProperties['justifyContent']
  flex?: CSSProperties['flex']
  gap?: CSSProperties['gap']
  height?: CSSDimension
  horizontal?: boolean
  justify?: CSSProperties['justifyContent']
  padding?: CSSProperties['padding']
  paddingBlock?: CSSProperties['paddingBlock']
  paddingInline?: CSSProperties['paddingInline']
  prefixCls?: string
  visible?: boolean
  width?: CSSDimension
  wrap?: CSSProperties['flexWrap']
}

function flexDirection(
  direction: FlexboxProps['direction'],
  horizontal: boolean | undefined
): CSSProperties['flexDirection'] {
  if (horizontal || direction === 'horizontal') return 'row'
  if (direction === 'horizontal-reverse') return 'row-reverse'
  if (direction === 'vertical-reverse') return 'column-reverse'
  return 'column'
}

export const Flexbox = forwardRef<HTMLElement, FlexboxProps>(function Flexbox(
  {
    align = 'stretch',
    allowShrink = false,
    as: Container = 'div',
    children,
    className,
    direction,
    distribution,
    flex = '0 1 auto',
    gap = 0,
    height = 'auto',
    horizontal,
    justify,
    padding = 0,
    paddingBlock,
    paddingInline,
    prefixCls,
    style,
    visible = true,
    width = 'auto',
    wrap = 'nowrap',
    ...rest
  },
  ref
) {
  const classes = ['growthos-lobe-flex-adapter', prefixCls && `${prefixCls}-flex`, className]
    .filter(Boolean)
    .join(' ')
  return (
    <Container
      {...rest}
      className={classes}
      ref={ref}
      style={{
        alignItems: align,
        display: visible ? 'flex' : 'none',
        flex,
        flexDirection: flexDirection(direction, horizontal),
        flexWrap: wrap,
        gap,
        height,
        justifyContent: justify ?? distribution ?? 'flex-start',
        minWidth: allowShrink ? 0 : undefined,
        padding,
        paddingBlock: paddingBlock ?? padding,
        paddingInline: paddingInline ?? padding,
        width,
        ...style,
      }}
    >
      {children}
    </Container>
  )
})

export type CenterProps = Omit<FlexboxProps, 'align' | 'justify'>

export const Center = forwardRef<HTMLElement, CenterProps>(function Center(props, ref) {
  return <Flexbox {...props} align='center' justify='center' ref={ref} />
})

type SVGIconProps = {
  className?: string
  color?: string
  fill?: string
  fillOpacity?: number | string
  fillRule?: 'evenodd' | 'nonzero'
  focusable?: boolean | 'false' | 'true'
  height?: number | string
  ref?: Ref<SVGSVGElement>
  size?: number | string
  strokeWidth?: number | string
  style?: CSSProperties
  width?: number | string
}

export interface IconProps extends HTMLAttributes<HTMLSpanElement> {
  color?: string
  fill?: string
  fillOpacity?: number | string
  fillRule?: 'evenodd' | 'nonzero'
  focusable?: boolean | 'false' | 'true'
  icon?: ComponentType<SVGIconProps> | ReactElement
  size?: number | string
  spin?: boolean
}

export const Icon = forwardRef<SVGSVGElement, IconProps>(function Icon(
  {
    className,
    color,
    fill = 'transparent',
    fillOpacity,
    fillRule,
    focusable,
    icon,
    size = 16,
    spin = false,
    style,
    ...rest
  },
  ref
) {
  const classes = [spin && 'growthos-lobe-icon-spin', className].filter(Boolean).join(' ')
  let child: ReactNode = null
  if (isValidElement(icon)) {
    child = icon
  } else if (icon) {
    const SVGIcon = icon
    child = (
      <SVGIcon
        color={color}
        fill={fill}
        fillOpacity={fillOpacity}
        fillRule={fillRule}
        focusable={focusable}
        height={size}
        ref={ref}
        size={size}
        width={size}
      />
    )
  }
  return (
    <span {...rest} className={classes || undefined} role='img' style={style}>
      {child}
    </span>
  )
})

export interface TagProps extends HTMLAttributes<HTMLSpanElement> {
  color?: string
  icon?: ReactNode
  size?: 'small' | 'middle' | 'large'
  variant?: 'borderless' | 'filled' | 'outlined'
}

export const Tag = forwardRef<HTMLSpanElement, TagProps>(function Tag(
  { children, color, icon, onClick, size = 'middle', style, variant = 'filled', ...rest },
  ref
) {
  const height = size === 'small' ? 20 : size === 'large' ? 28 : 22
  return (
    <span
      {...rest}
      onClick={onClick}
      ref={ref}
      style={{
        alignItems: 'center',
        background: variant === 'borderless' ? 'transparent' : color,
        border: variant === 'borderless' ? 0 : '1px solid currentColor',
        borderRadius: size === 'large' ? 6 : 3,
        cursor: onClick ? 'pointer' : undefined,
        display: 'inline-flex',
        gap: '0.4em',
        height,
        justifyContent: 'center',
        lineHeight: 1.2,
        paddingInline: size === 'small' ? 4 : size === 'large' ? 12 : 7,
        userSelect: 'none',
        width: 'fit-content',
        ...style,
      }}
    >
      {icon}
      {children}
    </span>
  )
})
