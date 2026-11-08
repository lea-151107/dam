# 変更履歴

このファイルには、公開したバージョンごとの変更を書きます。
タグの番号は `Version`（`version.go`）と同じです。`Version` は、描画の出力が変わるたびに上がります。

## [0.6.34] - 2026-11-08

最初の公開です。

- Mermaid のソースを pure Go で描きます。画像（`Render`）と罫線文字（`RenderText`）の 2 種類を出力します。
- 22 種類の図に対応しています。flowchart、sequenceDiagram、classDiagram、stateDiagram、pie、gantt、gitGraph、mindmap、erDiagram、timeline などです。
- ラベルの `<b>`・`<i>`・`<u>` と Markdown 文字列を描き分けます。
- `theme` と `themeVariables` に対応しています。front matter と `%%{init}%%` のどちらでも指定できます。
- `MaxWidth` に収まらない図は、間隔を詰める、折り返す、文字を縮める、の順に試してから縮小します。
- 日本語のラベルは、`FaceFunc` に渡したフォントで描きます。
- 出力は決定的です。同じ入力・オプション・`Version` からは同じ画素が出ます。
- 依存は標準ライブラリと `golang.org/x/image`・`golang.org/x/text` だけです。cgo・外部コマンド・ネットワークは使いません。
- ライセンスは MIT です。

[0.6.34]: https://github.com/lea-151107/dam/releases/tag/v0.6.34
