import type { Alpha } from './typeUtils';

type Whitespace = ' ' | '\n' | '\t' | '\r' | '\f' | '\v';
type Separator = Whitespace | '-' | '_';

type TrimLeft<S extends string> = S extends `${ Whitespace }${ infer R }` ? TrimLeft<R> : S;
type TrimRight<S extends string> = S extends `${ infer R }${ Whitespace }` ? TrimRight<R> : S;
type Trim<S extends string> = TrimLeft<TrimRight<S>>;

type IsUpperAlpha<C extends string> =
  C extends Alpha<C>
    ? C extends Lowercase<C>
      ? false
      : true
    : false;

type InsertDashesForKebab<
  S extends string,
  IsFirst extends boolean = true,
> = S extends `${ infer C }${ infer Rest }`
  ? `${
    IsUpperAlpha<C> extends true
      ? IsFirst extends true
        ? Lowercase<C> // C is the first character, so don't insert a dash
        : `-${ Lowercase<C> }` // C is uppercase, so insert a dash before it
      : C extends Separator
        ? '-' // C is a separator, so replace it with a dash
        : C // C is not uppercase or separator
    }${
      InsertDashesForKebab<Rest, false>
    }`
  : S;

type CollapseSeparators<
  S extends string,
> = S extends `${ infer C }${ infer Rest }`
  ? C extends Separator
    ? Rest extends `${ Separator }${ string }`
      ? CollapseSeparators<Rest> // Multiple separators in a row
      : `${ C }${ CollapseSeparators<Rest> }` // Keep single separator
    : `${ C }${ CollapseSeparators<Rest> }`
  : S; // Base case: S is empty

/**
 * Transform a string into kebab-case (lowercase, dash-separated).
 * @note This is guaranteed to be the same output as the `kebabCase` function,
 * at least for ASCII strings.
 */
export type KebabCase<T extends string> =
  CollapseSeparators<InsertDashesForKebab<Trim<T>>>;

/**
 * Converts a string to kebab-case.
 * @example kebabCase('HelloWorld') == 'hello-world'
 */
export function kebabCase<T extends string>(input: T): KebabCase<T> {
  return input
    .trim()
    .replace(/./ug, (c, i) => (i > 0 && /[A-Z]/.test(c) ? '-' : '') + c.toLowerCase())
    .replace(/[\s_-]+/g, '-') as KebabCase<T>;
}
