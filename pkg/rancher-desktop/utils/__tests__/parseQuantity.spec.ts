import parseQuantity from '../parseQuantity';

describe('parseQuantity', () => {
  test('returns 0n for undefined/empty', () => {
    expect(parseQuantity(undefined)).toBe(0n);
    expect(parseQuantity('')).toBe(0n);
    expect(parseQuantity('   ')).toBe(0n);
  });

  test('parses plain bytes', () => {
    expect(parseQuantity('0')).toBe(0n);
    expect(parseQuantity('123')).toBe(123n);
    expect(parseQuantity('123B')).toBe(123n);
  });

  test('parses BinarySI', () => {
    expect(parseQuantity('1Ki')).toBe(1024n);
    expect(parseQuantity('1.5KiB')).toBe(1536n);
    expect(parseQuantity('2Mi')).toBe(2n * (2n ** 20n));
  });

  test('parses DecimalSI', () => {
    expect(parseQuantity('1k')).toBe(1_000n);
    expect(parseQuantity('1K')).toBe(1_000n);
    expect(parseQuantity('1M')).toBe(1_000_000n);
    expect(parseQuantity('1MB')).toBe(1_000_000n);
    expect(parseQuantity('1.5K')).toBe(1_500n);
  });

  test('parses DecimalSI sub-byte quantities (truncate toward zero)', () => {
    expect(parseQuantity('1m')).toBe(0n);
    expect(parseQuantity('1500m')).toBe(1n);
    expect(parseQuantity('1u')).toBe(0n);
    expect(parseQuantity('1n')).toBe(0n);
  });

  test('parses decimal exponent', () => {
    expect(parseQuantity('1e3')).toBe(1_000n);
    expect(parseQuantity('1.5e3')).toBe(1_500n);
    expect(parseQuantity('2E+3')).toBe(2_000n);
    expect(parseQuantity('2E-1')).toBe(0n);
    expect(parseQuantity('1e3B')).toBe(1_000n);
  });

  test('parses signed values', () => {
    expect(parseQuantity('+2Ki')).toBe(2n * 1024n);
    expect(parseQuantity('-1.5Ki')).toBe(-1536n);
  });

  test('parses with whitespace', () => {
    expect(parseQuantity('  10  MiB')).toBe(10n * (2n ** 20n));
    expect(parseQuantity('5 Gi')).toBe(5n * (2n ** 30n));
  });

  test('throws on unknown suffix', () => {
    expect(() => parseQuantity('1mi')).toThrow('Unknown units: mi');
    expect(() => parseQuantity('1Zi')).toThrow('Unknown units: Zi');
    expect(() => parseQuantity('1MBi')).toThrow('Unknown units: MBi');
  });

  test('throws on malformed quantity', () => {
    expect(() => parseQuantity('abc')).toThrow('Malformed quantity: abc');
    expect(() => parseQuantity('Ki')).toThrow('Malformed quantity: Ki');
    expect(() => parseQuantity('1..2')).toThrow('Malformed quantity: 1..2');
  });
});
