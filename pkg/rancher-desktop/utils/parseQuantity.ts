/**
 * Parse a Kubernetes-style quantity string into bytes.
 * @note As this does not use BigInt, numbers beyond the usual range
 * (Number.MIN/MAX_SAFE_INTEGER) will not be supported correctly.
 * @note This means exabytes (EB/EiB) and beyond will not be supported correctly.
 */
export default function parseQuantity(value: string | number | undefined): number {
  if (value === undefined) {
    return 0;
  }

  if (typeof value === 'number') {
    if (!Number.isFinite(value)) {
      throw new Error(`Malformed quantity: ${ value }`);
    }
    return value;
  }

  const trimmed = value.trim();

  if (!trimmed) {
    return 0;
  }

  const [numeral = ''] = /^[+-]?[0-9.]+(?:[eE][+-]?[0-9]+$)?/.exec(trimmed) ?? [];
  const suffix = trimmed.slice(numeral.length).trim();
  if (!numeral) {
    throw new Error(`Malformed quantity: ${ trimmed }`);
  }

  const scaleMapping: Record<string, number> = {
    '': 1,
    n:  10 ** -9,
    u:  10 ** -6,
    m:  10 ** -3,
    k:  10 ** 3,
    M:  10 ** 6,
    G:  10 ** 9,
    T:  10 ** 12,
    P:  10 ** 15,
    E:  10 ** 18, // Loses precision.
    Ki: 2 ** 10,
    Mi: 2 ** 20,
    Gi: 2 ** 30,
    Ti: 2 ** 40,
    Pi: 2 ** 50,
    Ei: 2 ** 60, // Loses precision.
  };
  const scale: number | undefined = scaleMapping[suffix];

  if (typeof scale !== 'number') {
    throw new Error(`Unknown units: ${ suffix }`);
  }

  const parsedNumeral = Number(numeral);

  if (Number.isNaN(parsedNumeral)) {
    throw new Error(`Malformed quantity: ${ trimmed }`);
  }

  return parsedNumeral * scale;
}
