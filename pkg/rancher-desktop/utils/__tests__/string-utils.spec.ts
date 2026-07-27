import { KebabCase, kebabCase } from '../string-utils';

describe('kebabCase', () => {
  const cases = [
    ['camelCase', 'camel-case'],
    ['PascalCase', 'pascal-case'],
    ['word', 'word'],
    ['', ''],
    ['stringWith123Numbers', 'string-with123-numbers'],
    ['stringWith$pecialChars!', 'string-with$pecial-chars!'],
    ['HeLLoWorld', 'he-l-lo-world'],
    ['  string  ', 'string'],
    ['string   with   spaces', 'string-with-spaces'],
    ['string_with_underscores', 'string-with-underscores'],
    ['string-with-hyphens', 'string-with-hyphens'],
    ['string--with--multiple--hyphens', 'string-with-multiple-hyphens'],
    ['-prefix-suffix-', '-prefix-suffix-'],
    ['WSL Integration', 'w-s-l-integration'],
    // Emojis should not be affected.
    // U+1F426 U+200D U+1F525 bird + fire = phoenix
    // U+1D468 mathematical bold italic capital A
    // U+1F638 grinning cat face with smiling eyes
    ['\u{1F426}\u200D\u{1F525}\u{1D468}\u{1F638}', '\uD83D\uDC26\u200D\uD83D\uDD25\uD835\uDC68\uD83D\uDE38'],
  ] as const;
  it.each(cases)('should convert "%s" to "%s"', (input, expected) => {
    expect(kebabCase(input)).toBe(expected);
  });

  // TypeScript compile-time checks for KebabCase<T>
  type Equal<A, B> =
    (<T>() => T extends A ? 1 : 2) extends
    (<T>() => T extends B ? 1 : 2) ? true : false;

  type TupleKeys<T extends readonly unknown[]> = Exclude<keyof T, keyof any[]>;

  type TypeFailure<
    Input extends string,
    Actual extends string,
    Expected extends string,
  > = ['FAIL', { input: Input; actual: Actual; expected: Expected }];

  type CheckCase<C> =
    C extends readonly [infer Input extends string, infer Expected extends string]
      ? Equal<KebabCase<Input>, Expected> extends true
        ? never
        : TypeFailure<Input, KebabCase<Input>, Expected>
      : ['INVALID_CASE_SHAPE', C];

  type Failures = {
    [K in TupleKeys<typeof cases>]: CheckCase<(typeof cases)[K]>
  }[TupleKeys<typeof cases>];

  type AssertNoFailures<T extends never> = T;
  type _TypeTests = AssertNoFailures<Failures>;
});
