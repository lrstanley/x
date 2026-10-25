# Bundled fonts

The fonts in [`data/`](data) are embedded into the `fonts` package, gzipped.

| File | Source | License |
| --- | --- | --- |
| `JetBrainsMono-*.ttf.gz` | [JetBrains Mono](https://github.com/JetBrains/JetBrainsMono) 2.304 | SIL Open Font License 1.1 |
| `SymbolsNerdFontMono-Regular.ttf.gz` | [Nerd Fonts](https://github.com/ryanoasis/nerd-fonts) 3.4.0 symbols-only font | See the [Nerd Fonts license](https://github.com/ryanoasis/nerd-fonts/blob/master/LICENSE); glyph sets keep their upstream licenses |
| `DejaVuSansMonoSymbols-Regular.ttf.gz` | Symbol subset of [DejaVu Sans Mono](https://github.com/dejavu-fonts/dejavu-fonts) 2.37 | Bitstream Vera and Arev font licenses, DejaVu changes public domain; see [`data/DejaVuSansMonoSymbols-LICENSE`](data/DejaVuSansMonoSymbols-LICENSE) |
| `NotoColorEmoji-Subset.ttf.gz` | Subset of [Noto Color Emoji](https://github.com/googlefonts/noto-emoji) 2.057 | SIL Open Font License 1.1; see [`data/NotoColorEmoji-LICENSE`](data/NotoColorEmoji-LICENSE) |

## Noto Color Emoji subset

`NotoColorEmoji-Subset` draws emoji in color when no installed font does. It is
not a family that can be selected: it has no text glyphs, and the family index
skips it. It keeps the bitmaps of Noto Color Emoji's `CBDT`/`CBLC` tables for:

- every emoji, flag, keycap and subdivision flag, and the ZWJ sequences without
  a skin tone (couples, families, professions, the rainbow flag, and so on);
- but not the skin tone modifiers or the sequences that use them (about 57% of
  the original bitmaps). `fonts.BundledColorGlyph` draws these in the default
  tone instead.

To fit in about 2.6 MB instead of 10.7 MB, each bitmap is also scaled from
136×128 to 64×60 pixels and quantized to a 48 color palette (libimagequant, no
dithering), and the strike's metrics are scaled to match. The result is still
a valid `CBDT` font; the renderer only reads the PNG sizes. 64 pixels covers a
two cell wide emoji up to a 32 pixel cell; larger ones are upscaled and look
softer. Compressing the file barely helps (the PNGs are already deflated).

Noto Color Emoji declares no Reserved Font Name, so the subset keeps its name
and notices. The OFL text is embedded with the font as `data/NotoColorEmoji-LICENSE`.

To rebuild it from `NotoColorEmoji.ttf` (fontTools 4.x, Pillow with
libimagequant):

```sh
python3 -I -c '
import io, sys
from fontTools import subset
from fontTools.ttLib import TTFont
from PIL import Image

WIDTH, COLORS = 64, 48
font = TTFont(sys.argv[1])
unicodes = sorted(cp for cp in font.getBestCmap() if not 0x1F3FB <= cp <= 0x1F3FF)

opts = subset.Options()
opts.layout_features = ["ccmp", "rlig", "liga", "clig"]
opts.name_IDs = [0, 1, 2, 3, 4, 5, 6, 13, 14]
opts.name_languages = ["*"]
opts.notdef_outline = True
opts.glyph_names = False
sub = subset.Subsetter(opts)
sub.populate(unicodes=unicodes)
sub.subset(font)

old = font["CBLC"].strikes[0].bitmapSizeTable.ppemX
scale = None
for data in font["CBDT"].strikeData:
    for glyph in data.values():
        im = Image.open(io.BytesIO(glyph.imageData)).convert("RGBA")
        scale = scale or WIDTH / im.width
        h = max(1, round(im.height * scale))
        im = im.resize((WIDTH, h), Image.LANCZOS).quantize(
            colors=COLORS, method=Image.Quantize.LIBIMAGEQUANT, dither=Image.Dither.NONE)
        buf = io.BytesIO()
        im.save(buf, "PNG", optimize=True)
        glyph.imageData = buf.getvalue()
        m = glyph.metrics
        m.width, m.height = WIDTH, h
        m.BearingX, m.BearingY, m.Advance = (round(v * scale) for v in (m.BearingX, m.BearingY, m.Advance))

for strike in font["CBLC"].strikes:
    t = strike.bitmapSizeTable
    t.ppemX = t.ppemY = max(1, round(old * scale))
    for lm in (t.hori, t.vert):
        for name in ("ascender", "descender", "widthMax", "caretOffset"):
            setattr(lm, name, round(getattr(lm, name) * scale))
font.save(sys.argv[2])
' NotoColorEmoji.ttf NotoColorEmoji-Subset.ttf
gzip -9n NotoColorEmoji-Subset.ttf
```

## DejaVu Sans Mono Symbols

`DejaVuSansMonoSymbols-Regular` is the fallback for symbols that neither the
primary font nor the Symbols Nerd Font has (stars, check marks, weather, music,
geometric and arrow variants, and so on). It keeps only these blocks of
DejaVu Sans Mono 2.37:

```
U+2000-206F General Punctuation       U+2400-243F Control Pictures
U+2070-209F Superscripts/Subscripts   U+2440-245F OCR
U+20A0-20CF Currency Symbols          U+2460-24FF Enclosed Alphanumerics
U+2100-214F Letterlike Symbols        U+25A0-25FF Geometric Shapes
U+2150-218F Number Forms              U+2600-26FF Miscellaneous Symbols
U+2190-21FF Arrows                    U+2700-27BF Dingbats
U+2200-22FF Mathematical Operators    U+27C0-27FF Misc Math A, Supp. Arrows A
U+2300-23FF Miscellaneous Technical   U+2900-2BFF Supp. Arrows B, Misc Math B,
                                                  Supp. Math, Misc Symbols/Arrows
```

The Bitstream Vera and Arev licenses only allow modified fonts to be
distributed under names that don't contain "Bitstream", "Vera", "Arev", or
"Tavmjong Bah". The subset is renamed to "DejaVu Sans Mono Symbols", which also
keeps it from shadowing an installed DejaVu Sans Mono when fonts are looked up
by family name. The copyright and license notices are kept in the font's name
table and in the license file next to it, which is embedded with the font.

To rebuild it from `dejavu-fonts-ttf-2.37.zip` (fontTools 4.x):

```sh
python3 -c '
import sys
from fontTools import subset
from fontTools.ttLib import TTFont

font = TTFont(sys.argv[1])
opts = subset.Options()
opts.hinting = False
opts.desubroutinize = True
opts.layout_features = ["*"]
opts.name_IDs = ["*"]
opts.name_languages = ["*"]
opts.notdef_outline = True
sub = subset.Subsetter(opts)
sub.populate(unicodes=subset.parse_unicodes(
    "U+2000-206F,U+2070-209F,U+20A0-20CF,U+2100-214F,U+2150-218F,U+2190-21FF,"
    "U+2200-22FF,U+2300-23FF,U+2400-243F,U+2440-245F,U+2460-24FF,U+25A0-25FF,"
    "U+2600-26FF,U+2700-27BF,U+27C0-27EF,U+27F0-27FF,U+2900-297F,U+2980-29FF,"
    "U+2A00-2AFF,U+2B00-2BFF"))
sub.subset(font)

family, ps = "DejaVu Sans Mono Symbols", "DejaVuSansMonoSymbols"
version = font["name"].getDebugName(5) or ""
names = font["name"]
for rec in list(names.names):
    if rec.nameID in (1, 3, 4, 6, 16, 17, 18, 21, 22):
        names.removeNames(nameID=rec.nameID)
for pid, eid, lid in ((3, 1, 0x409), (1, 0, 0)):
    names.setName(family, 1, pid, eid, lid)
    names.setName("Book", 2, pid, eid, lid)
    names.setName(f"{ps}: {version}", 3, pid, eid, lid)
    names.setName(family, 4, pid, eid, lid)
    names.setName(ps, 6, pid, eid, lid)
    names.setName(
        "Symbol subset of DejaVu Sans Mono 2.37, renamed per the Bitstream Vera "
        "and Arev font licenses. Bundled with github.com/lrstanley/x/term/still.",
        10, pid, eid, lid)
font.save(sys.argv[2])
' dejavu-fonts-ttf-2.37/ttf/DejaVuSansMono.ttf DejaVuSansMonoSymbols-Regular.ttf
gzip -9n DejaVuSansMonoSymbols-Regular.ttf
```
