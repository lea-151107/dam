# コーディング規約

このリポジトリ（pure Go の Mermaid レンダラ）のコードはすべて、この規約に従って書く。
背景と全体設計は [plans/mermaid-renderer.md](plans/mermaid-renderer.md) と [RFP.txt](RFP.txt) を参照。

---

## 1. 制約（必ず守る）

| 項目 | ルール |
|---|---|
| 依存 | 標準ライブラリと `golang.org/x/image` だけを使う。x/image が内部で使う `golang.org/x/text`・`golang.org/x/sys/cpu`（いずれも pure Go）は間接依存として許容する。新しい依存を追加する場合は事前に合意を取る。`//go:build ignore` の生成プログラム（例 `internal/textgrid/gen_width.go`）に限り、x/text を直接 import してよい |
| cgo | 禁止。`CGO_ENABLED=0 go build ./...` が常に通ること |
| 外部プロセス・ネットワーク | `os/exec` と `net` 以下のパッケージは import しない |
| `unsafe` | 自分たちのコードでは import しない（標準ライブラリ内部での利用は対象外） |
| ファイル I/O | ライブラリ本体（`cmd/` 以外）では `os` を使ってファイルを読み書きしない。フォントなどの埋め込みデータは `x/image` が提供するもの、または `embed` を使う |
| グローバル状態 | 実行中に書き換えるパッケージ変数を持たない（読み取り専用の参照テーブルと、`regexp.MustCompile` で作る正規表現は可）。`init()` で副作用を起こさない（キャッシュなども呼び出しごとに作るか、`Options` 経由で渡す） |
| Go のバージョン | `go.mod` の `go` ディレクティブに書いたバージョンを最低要件とする |

これらは `deps_test.go` が自動で確認する（依存パッケージの一覧に禁止パッケージがないか）。

## 2. 整形と静的解析

- `gofmt -s` と `goimports` を通す。import は「標準ライブラリ」「外部モジュール」「このモジュール内」の 3 グループに分ける。
- `go vet ./...` で警告がないこと。
- `staticcheck` は推奨するが任意。`go.mod` には追加せず、`go run honnef.co/go/tools/cmd/staticcheck@<version>` のようにツールとして実行する。
- 1 行の長さに上限は設けないが、120 桁を超えそうなら改行を検討する。

## 3. パッケージ構成と依存の向き

```
mermaid (ルート: 公開 API、図種の判定、MaxWidth に収める戦略、エラーの変換)
  ├─ internal/diagram/<種類>   パーサ + レイアウト → scene、テキスト描画（text.go）
  │    ├─ internal/diagram       全図種で共有する Env・Layouter・Finish
  │    ├─ internal/layout/sugiyama, internal/layout/tree
  │    │                         図種に依存しないレイアウトアルゴリズム
  │    ├─ internal/source        行分割・トークナイザ
  │    ├─ internal/boxgraph      箱とエッジの図（ER、要件、C4、アーキテクチャ）の画像と罫線出力のモデル
  │    ├─ internal/chart         グラフの目盛り（1-2-5 系列）と数値の書式
  │    ├─ internal/textgraph     ノードとエッジの図の罫線出力（経路探索を含む）
  │    ├─ internal/textgrid      テキスト描画用の文字グリッドと文字幅
  │    ├─ internal/textmeasure   font.Face によるテキスト計測
  │    │    └─ internal/outline  内蔵フォントの Face（グリフを決定的に描く）
  │    │         └─ internal/curve  曲線を整数演算で折れ線にする
  │    └─ internal/theme         色
  ├─ internal/raster            scene → *image.RGBA
  └─ internal/scene             描画命令（px）
最下層: internal/guard（上限と ctx）、internal/richtext（ラベルの太字・斜体・下線の印）
```

依存の向きは `deps_test.go` の `TestLayering` が `layers` の表で確認する。パッケージを追加・変更したら、この図と表を同時に更新する。

- 公開 API はルートパッケージ `mermaid` だけに置く。それ以外はすべて `internal/` に置く。
- 依存は上から下への一方向だけにする。下位のパッケージが上位を import してはいけない（例: `raster` は `diagram/...` を知らない）。
- `internal/diagram/<種類>` どうしは互いに import しない。共有したい処理は `layout` などの下位パッケージに移す。
- 1 つの図種のコードは、原則として `parse.go`（AST とパーサ）、`layout.go`（AST → scene）、`text.go`（AST → テキスト。`RenderText` 対応の図種のみ）、`*_test.go` に分ける。小さな図種（pie、mindmap）は 1 ファイルにまとめてよい。
- テキスト描画は図種ごとのレイアウトの知識が要るため、出力ごとのパッケージには分けず、図種パッケージに置く。画像とテキストに共通する処理は `layout` や `textgrid` に移す。

## 4. 命名

- Go の慣習に従う: MixedCaps を使い、頭字語は大文字でそろえる（`ID`, `URL`, `RGBA`, `SVG`）。
- パッケージ名は短い小文字の単数形にする。`util`, `common`, `helpers`, `misc` は禁止。
- ファイル名は小文字にする。区切りが必要なら `_` を使う（Go の慣習どおり `_test.go`, `_amd64.go` と衝突しないように注意）。
- 単位を持つ値は、名前で単位を示す: `widthPx`, `fontSizePt`, `durationDays`。
- レシーバ名は型名の頭文字 1〜2 文字にし、同じ型の中で統一する。

## 5. エラー処理

- 番兵エラーは `ErrXxx` と命名し、ルートパッケージで公開する（`ErrUnsupported`, `ErrTooLarge`）。内部パッケージで新たに番兵を作る場合も、公開 API からは必ずルートの番兵に変換して返す。
- 構文エラーは必ず `*mermaid.SyntaxError` で返す。`Line` は **1 から数え、元のソース（front matter を含む）での行番号** とする。
- ラップには `fmt.Errorf("...: %w", err)` を使い、呼び出し側が `errors.Is` / `errors.As` で判別できるようにする。
- エラーメッセージは小文字で始め、末尾に句点やピリオドを付けない。ルートが返すメッセージには `mermaid: ` を前置する。
- `context` のキャンセルは `ctx.Err()` をそのまま（ラップしてもよいが `%w` で）返す。
- ライブラリのコードでは `panic` しない。想定外の panic に備え、`Render` / `RenderText` の境界で `recover` してエラーに変換する（その際スタックはメッセージに含めない）。
- エラーを握りつぶさない。意図的に無視する場合は `_ =` で受けて、理由をコメントに書く。

## 6. 決定性（同じ入力から同じ画像を出す）

gvmd は「入力の sha256 + `Version`」をキャッシュのキーにするので、出力が揺れるとキャッシュが壊れる。

- map を反復した順序を出力に影響させない。順序が必要なら、キーをソートするか、挿入順を持つスライスと併用する。
- 結果の順序が出力に影響するソートは `sort.SliceStable` / `slices.SortStableFunc` を使い、比較関数には必ず最後のタイブレーク（例: 元の出現順）を入れる。要素が比較キーで完全に区別できる場合や、順序が結果に影響しない場合（交差数を数えるだけ、など）は `slices.SortFunc` でよい。
- `math/rand`、`time.Now`、環境変数、ロケール、goroutine の完了順に依存しない。並列化する場合も、結果は決まった順序で結合する。
- レイアウト座標は raster に渡す前に 1/64 px（`fixed.Int26_6` 相当）に丸める。
- arm64 や `GOAMD64=v3` では、コンパイラが `a*b + c` を FMA に融合し、結果の最下位ビットが変わる。1/64 px に丸めてもこれは吸収しきれない（丸めの境界をまたぐ値がある）。そのため次を守る。
  - 曲線を `vector.Rasterizer` の `QuadTo` / `CubeTo` に渡さない。`internal/curve` で整数のまま折れ線にし、`LineTo` だけを使う（vector の曲線の分割は float32 で、融合の影響を受ける）。
  - グリフも同じ理由で `opentype.Face` に描かせない。内蔵フォントは `internal/outline` の Face で描く。呼び出し側が渡す Face（`Face` / `FaceFunc`）の決定性は、その Face の実装による。
  - float64 の積と商は `float64(a*b)` のように明示的に型変換して、融合を止める。融合は文や（インライン化された）関数をまたいで起きるので、丸める直前だけでなく、すべての積と商を対象にする。`TestNoFMA` が arm64 と `GOAMD64=v3` のアセンブリに FMA 命令がないことを確かめる。
  - 確認: `GOAMD64=v3 go test .` と `go test -tags noasm .` が、どちらも golden と一致すること（v3 は FMA の融合、noasm は vector の純 Go 実装を、amd64 の上で再現する）。
- 出力に影響する変更を加えたら `Version` を上げる。golden の更新が必要になった変更は、出力に影響する変更とみなす。`TestMain`（`goldenversion_test.go`）が `testdata/golden.version` に記録した Version と golden 全体のハッシュを確認し、Version を上げずに golden が変わると `-update` でも失敗する。

## 7. リソース保護

- 入力サイズに比例して回るループ（パース、レイアウトの反復、交差削減など）では `ctx.Err()` を確認する。毎回でなくてよく、`guard.Tick()` を使う（16 回に 1 回確認する）。
- 反復回数、ノード数、入力長、画像サイズには上限を設け、名前付きの定数として定義する（例: `maxCrossingSweeps`, `maxInputBytes`, `maxImagePixels`）。
- 上限を超えたら `ErrTooLarge` を返す。上限で処理を黙って打ち切り、不完全な画像を返すことはしない。
- 再帰は深さが入力に依存しうる箇所（ネストした subgraph、複合 state、mindmap など）では深さ制限を設けるか、明示的なスタックに書き換える。

## 8. 数値と単位

- レイアウトは `float64` で計算する。単位は「px（Scale = 1 のとき）」とし、`Scale` は raster で一度だけ掛ける。テキストの計測結果は `textmeasure` が Scale で割ってレイアウト単位に戻す。
- テキストの計測は `font.Face` の `fixed.Int26_6` で行い、レイアウトに渡すときに float64 に変換する。変換処理は `textmeasure` に集約する。
- 見た目を調整する値（余白、間隔、最小幅、線幅など、変えると図の印象が変わるもの）は名前付き定数にして、図種ごとの `layout.go` の冒頭にまとめる（`nodePaddingPx = 8` など）。
- 1 か所でしか使わない幾何の微調整（アイコンの中の点の位置、タブの切り欠きなど）は、その場のリテラルでよい。ただし、同じ値が 2 か所以上に出てくるか、他の定数と連動して変える必要があるなら名前を付ける。
- 浮動小数点の比較に `==` を使わない。許容誤差の定数を使う。

## 9. コメントとドキュメント

- 公開識別子には godoc コメントを必ず付ける（英語）。識別子名で始める: `// Render draws ...`。
- 内部のコメントは日本語でも英語でもよい。**何をするかではなく、なぜそうするか** を書く。
- Mermaid 本家と挙動を意図的に変えている箇所には `// NOTE(mermaid-compat):` を付けて理由を書く。
- 未対応の構文には `// TODO(<図種>):` を付ける。TODO を書く代わりにその構文を黙って無視することはしない（`ErrUnsupported` か `SyntaxError` を返す）。

## 10. テスト

- テーブル駆動にし、`t.Run(name, ...)` でサブテストに名前を付ける。
- 入力の Mermaid は `testdata/<図種>_<ケース>.mmd` に置き、golden 画像は `testdata/golden/<同名>.png` に置く。`go test ./... -update` で golden を再生成する。
- golden を更新したら、差分画像を目視で確認してからコミットする。
- 新しい構文に対応するときは、最低限 **golden ケース 1 つ** と **構文エラーのケース 1 つ**（行番号の確認を含む）を追加する。
- 各パーサには `FuzzParse` を用意する。panic しないこと、上限内の時間で返ることを確認する。
- 決定性テスト（同じ入力を複数回・並行して描画し、ピクセルが一致すること）と出力幅テスト（`Dx() <= MaxWidth`）は全 golden ケースに適用する。
- テストでは外部のフォントファイルに依存しない。日本語のテストが必要な場合は、ライセンス上問題のない小さなフォントのサブセットを `testdata/` に置く。

## 11. コミット

- 1 コミットに 1 つの論理的な変更を入れる。リファクタリングと機能追加は分ける。
- メッセージは `<パッケージ or 図種>: <要約>` の形式で書く（例: `flowchart: support subgraph`, `raster: fix miter join overflow`）。要約は英語で、命令形・小文字で始める。
- コミット前に `gofmt`, `go vet ./...`, `go test ./...` が通ること。
