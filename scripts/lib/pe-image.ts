/**
 * Reading the structure of Windows PE images.
 */

/**
 * Return the section names of a PE image, in section-table order.  A name
 * longer than eight bytes comes back as its string-table reference, such as
 * `/4`.
 */
export function peSectionNames(image: Buffer): string[] {
  if (image.length < 0x40 || image.toString('latin1', 0, 2) !== 'MZ') {
    throw new Error('Not a PE image: missing MZ header');
  }
  const peOffset = image.readUInt32LE(0x3c);
  const coffOffset = peOffset + 4;

  if (coffOffset + 20 > image.length || image.toString('latin1', peOffset, coffOffset) !== 'PE\0\0') {
    throw new Error('Not a PE image: missing PE signature');
  }
  const sectionCount = image.readUInt16LE(coffOffset + 2);
  const optionalHeaderSize = image.readUInt16LE(coffOffset + 16);
  const tableOffset = coffOffset + 20 + optionalHeaderSize;

  if (tableOffset + sectionCount * 40 > image.length) {
    throw new Error('Truncated PE section table');
  }

  return Array.from({ length: sectionCount }, (_, i) => {
    const name = image.toString('latin1', tableOffset + i * 40, tableOffset + i * 40 + 8);

    return name.split('\0')[0];
  });
}
