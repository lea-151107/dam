# テスト用フォント

`NotoSansCJKjp-subset.otf` は、日本語の golden（`testdata/*_ja.mmd`）を描くためだけのフォントです。
ライブラリには含まれません（Go は `testdata` をビルドしません）。

- 元のフォント: Noto Sans CJK JP Regular（`NotoSansCJK-Regular.ttc` の番号 0）
- ライセンス: SIL Open Font License 1.1（[OFL.txt](OFL.txt)）。予約フォント名の指定はありません。
- 含む文字: ASCII、和文の記号（U+3000–303F）、ひらがな、カタカナ、全角英数・記号（U+FF01–FF5E）、次の漢字

```
付件促信入再利力功受号基定実後成払支文本注用画番発示義者行表装要計設証認返送開？
```

## 作り直し方

`_ja` のテストで上の一覧にない漢字を使うときは、サブセットを作り直します。fontTools はこのときだけ使います（ライブラリの依存ではありません）。

```sh
python3 -m venv /tmp/ft && /tmp/ft/bin/pip install fonttools
# テストで使う文字をすべて chars.txt に集める（ASCII と testdata/*_ja.mmd の文字）
/tmp/ft/bin/pyftsubset /usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc --font-number=0 \
  --text-file=chars.txt --unicodes='U+3000-303F,U+3041-3096,U+309B-309F,U+30A0-30FF,U+FF01-FF5E' \
  --layout-features='' --no-hinting --desubroutinize --name-IDs='*' \
  --output-file=testdata/fonts/NotoSansCJKjp-subset.otf
```

フォントを作り直すと `_ja` の golden が変わるので、`Version` を上げてから `go test -update` を実行します。
