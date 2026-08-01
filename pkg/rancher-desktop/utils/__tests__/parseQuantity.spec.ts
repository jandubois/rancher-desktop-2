import parseQuantity from '../parseQuantity';

describe('parseQuantity', () => {
  test('returns 0 for undefined/empty', () => {
    expect(parseQuantity(undefined)).toBe(0);
    expect(parseQuantity('')).toBe(0);
    expect(parseQuantity('   ')).toBe(0);
  });

  test('accepts numbers', () => {
    expect(parseQuantity(0)).toBe(0);
    expect(parseQuantity(123)).toBe(123);
    expect(parseQuantity(-456)).toBe(-456);
  });

  test('parses plain bytes', () => {
    expect(parseQuantity('0')).toBe(0);
    expect(parseQuantity('123')).toBe(123);
  });

  test('parses BinarySI', () => {
    expect(parseQuantity('1Ki')).toBe(1024);
    expect(parseQuantity('1.5Ki')).toBe(1536);
    expect(parseQuantity('2Mi')).toBe(2 * (2 ** 20));
  });

  test('parses DecimalSI', () => {
    expect(parseQuantity('1k')).toBe(1_000);
    expect(parseQuantity('1M')).toBe(1_000_000);
    expect(parseQuantity('1.5G')).toBe(1_500_000_000);
  });

  test('parses DecimalSI sub-byte quantities', () => {
    expect(parseQuantity('1m')).toBe(0.001);
    expect(parseQuantity('1500m')).toBe(1.5);
    expect(parseQuantity('1u')).toBe(0.000001);
    expect(parseQuantity('1n')).toBe(0.000000001);
  });

  test('parses decimal exponent', () => {
    expect(parseQuantity('1e3')).toBe(1_000);
    expect(parseQuantity('1.5e3')).toBe(1_500);
    expect(parseQuantity('2E+3')).toBe(2_000);
    expect(parseQuantity('2E-1')).toBe(0.2);
  });

  test('parses signed values', () => {
    expect(parseQuantity('+2Ki')).toBe(2 * 1024);
    expect(parseQuantity('-1.5Ki')).toBe(-1536);
  });

  test('parses with whitespace', () => {
    expect(parseQuantity('  10  Mi')).toBe(10 * (2 ** 20));
    expect(parseQuantity('5 Gi')).toBe(5 * (2 ** 30));
  });

  test('accepts trailing dot', () => {
    expect(parseQuantity('10.G')).toBe(10_000_000_000);
  });

  test('accepts leading dot', () => {
    expect(parseQuantity('.5k')).toBe(500);
  });

  test('accepts limits of Number', () => {
    expect(parseQuantity(`${ Number.MAX_SAFE_INTEGER }`)).toBe(Number.MAX_SAFE_INTEGER);
    expect(parseQuantity(`${ Number.MIN_SAFE_INTEGER }`)).toBe(Number.MIN_SAFE_INTEGER);
  });

  test('throws on invalid numbers', () => {
    expect(() => parseQuantity(parseFloat('NaN'))).toThrow('Malformed quantity: NaN');
    expect(() => parseQuantity(Number.POSITIVE_INFINITY)).toThrow('Malformed quantity: Infinity');
  });

  test('throws on unknown suffix', () => {
    expect(() => parseQuantity('1mi')).toThrow('Unknown units: mi');
    expect(() => parseQuantity('1Zi')).toThrow('Unknown units: Zi');
    expect(() => parseQuantity('1MBi')).toThrow('Unknown units: MBi');
    expect(() => parseQuantity('1toString')).toThrow('Unknown units: toString');
  });

  test('throws on scientific notation with suffix', () => {
    expect(() => parseQuantity('1e3Ki')).toThrow('Unknown units: e3Ki');
    expect(() => parseQuantity('2.5E-1Mi')).toThrow('Unknown units: E-1Mi');
  });

  test('throws on malformed quantity', () => {
    expect(() => parseQuantity('abc')).toThrow('Malformed quantity: abc');
    expect(() => parseQuantity('Ki')).toThrow('Malformed quantity: Ki');
    expect(() => parseQuantity('1..2')).toThrow('Malformed quantity: 1..2');
    expect(() => parseQuantity('1.2.3')).toThrow('Malformed quantity: 1.2.3');
  });
});
