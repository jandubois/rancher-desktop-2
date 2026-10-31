import { RcIconType } from '@pkg/types/RcIcon';

import type { RouteLocationRaw } from 'vue-router';

export interface NavigationProps { to?: RouteLocationRaw; href?: string }

// TODO: 13211 Investigate why `InstanceType<typeof RcButton>` fails prod builds
// export type RcButtonType = InstanceType<typeof RcButton>
export interface RcButtonType {
  focus: () => void;
}

export type ButtonVariant = 'primary' | 'secondary' | 'tertiary' | 'link' | 'multiAction' | 'ghost';

export interface ButtonVariantNewProps {
  variant?: ButtonVariant;
}

/**
 * @deprecated Use the `variant` property instead. These boolean props will be removed in a future version.
 */
export interface ButtonVariantProps {
  primary?:     boolean;
  secondary?:   boolean;
  tertiary?:    boolean;
  link?:        boolean;
  multiAction?: boolean;
  ghost?:       boolean;

}

export type ButtonSize = 'small' | 'medium' | 'large';

export interface ButtonSizeNewProps {
  size?: ButtonSize;
}

/**
 * @deprecated Use the `size` property instead. The `small` boolean prop will be removed in a future version.
 */
export interface ButtonSizeProps {
  small?: boolean;
}

export interface IconProps {
  leftIcon?:  RcIconType;
  rightIcon?: RcIconType;
}
