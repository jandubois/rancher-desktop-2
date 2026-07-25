/**
 * Parse a Kubernetes-style quantity string into bytes.
 * @note Values are truncated toward zero.
 */
export default function parseQuantity(value: string | undefined): bigint {
  if (value === undefined) {
    return 0n;
  }

  const trimmed = value.trim();

  if (!trimmed) {
    return 0n;
  }

  const [, numeral, rawSuffix] = /^([+-]?[0-9.]+)\s*([^0-9.]\S*)?$/.exec(trimmed) ?? [];
  if (numeral === undefined) {
    throw new Error(`Malformed quantity: ${ trimmed }`);
  }
  const suffix = rawSuffix?.replace(/B$/, '') ?? ''; // remove trailing B if present

  let scale = 1n; // Negative scale means divide.
  const scaleMapping: Record<string, bigint> = {
    '': 1n,
    n:  -(10n ** 9n),
    u:  -(10n ** 6n),
    µ:  -(10n ** 6n),
    μ:  -(10n ** 6n),
    m:  -(10n ** 3n),
    k:  10n ** 3n,
    K:  10n ** 3n,
    M:  10n ** 6n,
    G:  10n ** 9n,
    T:  10n ** 12n,
    P:  10n ** 15n,
    E:  10n ** 18n,
    Ki: 2n ** 10n,
    Mi: 2n ** 20n,
    Gi: 2n ** 30n,
    Ti: 2n ** 40n,
    Pi: 2n ** 50n,
    Ei: 2n ** 60n,
  };

  if (suffix in scaleMapping) {
    scale = scaleMapping[suffix];
  } else {
    const [, sign, exponent] = /^[eE]([+-]?)(\d+)$/.exec(suffix) ?? [];
    if (exponent !== undefined) {
      scale *= 10n ** BigInt(exponent);
      if (sign === '-') {
        scale = -scale;
      }
    } else {
      throw new Error(`Unknown units: ${ rawSuffix || trimmed }`);
    }
  }

  const [, sign, whole, fraction = '0'] = /^([+-]?)([0-9]+)?(?:\.([0-9]+))?$/.exec(numeral) ?? [];

  if (whole === undefined) {
    throw new Error(`Malformed quantity: ${ trimmed }`);
  }

  const denom = 10n ** BigInt(fraction.length);
  const numer = (BigInt(whole) * denom + BigInt(fraction)) * (sign === '-' ? -1n : 1n);

  if (scale < 0n) {
    return numer / (-scale * denom);
  }

  return (numer * scale) / denom;
}
