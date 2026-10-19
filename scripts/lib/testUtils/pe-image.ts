/** Build a minimal PE image whose section table holds the given names. */
export function makePEImage(names: string[]): Buffer {
  const optionalHeaderSize = 0xf0;
  const image = Buffer.alloc(0x80 + 4 + 20 + optionalHeaderSize + names.length * 40);

  image.write('MZ', 0, 'latin1');
  image.writeUInt32LE(0x80, 0x3c);
  image.write('PE\0\0', 0x80, 'latin1');
  image.writeUInt16LE(names.length, 0x84 + 2);
  image.writeUInt16LE(optionalHeaderSize, 0x84 + 16);
  names.forEach((name, i) => {
    image.write(name, 0x84 + 20 + optionalHeaderSize + i * 40, 'latin1');
  });

  return image;
}
