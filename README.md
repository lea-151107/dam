# mermaid (pure Go)

Mermaid のソースを `image.Image` に描画する、pure Go のライブラリです。
cgo・外部コマンド・ネットワークは使いません。依存は標準ライブラリと `golang.org/x/image`・`golang.org/x/text` だけです
（`golang.org/x/text` は結合文字の合成（`unicode/norm`）に直接使い、x/image も内部で使います。`golang.org/x/sys/cpu` は間接的に入ります。どちらも pure Go です）。

## 対応している図

| 図 | 画像 (`Render`) | 罫線文字 (`RenderText`) |
|---|---|---|
| flowchart / graph | ✓ | ✓（点線は ┄┆、太線は ━┃） |
| sequenceDiagram | ✓ | ✓（アクティベーションは ┃。box の背景は描かない） |
| classDiagram | ✓ | ✓（区画付きの箱、△ ◆ ◇ の印） |
| stateDiagram / stateDiagram-v2 | ✓ | ✓（● ◉ ◇ ━、複合状態は枠） |
| pie | ✓ | ✓（横棒グラフ） |
| gantt | ✓ | ✓ |
| gitGraph | ✓ | ✓（ブランチごとの縦の列） |
| mindmap | ✓ | ✓（├─ └─ の木） |
| erDiagram | ✓ | ✓（多重度はラベルに 1・0..1・1..*・0..*） |
| timeline | ✓ | ✓（期間と出来事の一覧） |
| journey | ✓ | ✓（●○ の点数付きの表） |
| quadrantChart | ✓ | ✓（象限ごとの点の一覧） |
| xychart-beta | ✓ | ✓（系列ごとの横棒） |
| requirementDiagram | ✓ | ✓ |
| C4Context / C4Container / C4Component / C4Dynamic / C4Deployment | ✓ | ✓ |
| kanban | ✓ | ✓（列ごとのカードの一覧） |
| block-beta | ✓ | ✓（格子。エッジは下に一覧） |
| packet-beta | ✓ | ✓（幅が足りなければフィールドの一覧） |
| sankey-beta | ✓ | ✓（流れの一覧と横棒） |
| radar-beta | ✓ | ✓（軸×系列の表） |
| treemap-beta | ✓ | ✓（値付きの木） |
| architecture-beta | ✓ | ✓ |

上記以外（zenuml など）は `ErrUnsupported` を返します。

### 書き方の対応

- front matter（`---` で囲んだ YAML）の `title` は図の上に描きます。ブロック文字列（`title: |`、`>`）とフロー形式（`config: {theme: dark}`）も読みます。`config.theme` と `config.themeVariables`、`%%{init: {...}}%%` のテーマ指定も効きます（下の「テーマ」）。
- ラベルの `<b>`・`<strong>`（太字）、`<i>`・`<em>`（斜体）、`<u>`（下線）と、Markdown 文字列（`` "`**太字** と *斜体*`" ``）を描き分けます。Markdown 文字列は途中で改行できます。
- flowchart: Mermaid 11 の `A@{ shape: 名前, label: "…" }`（Mermaid 11 の形と別名。fork・join・bang・cloud を除く）、エッジ ID（`A e1@--> B`）と `e1@{ curve: linear }`、サブグラフごとの `direction`（サブグラフの中のノードが外とつながっていないときだけ。本家と同じ）。
  - エッジのラベルは、空白の有無にかかわらず `A--Yes-->B` も `A -- Yes --> B` も読みます。
  - 本家の lexer と同じく、`--` の直後の `o` / `x` は、後ろに名前が続いても端の印です（`A---oB` は B への丸付きの線、`dev---ops` は「ps」への線）。
  - 端の印も閉じも無い `A--B` は構文エラーです。
- sequenceDiagram:
  - `title`、`box`（色付き）、`create participant` / `destroy`
  - アクティベーション（罫線出力では太線 `┃`）。アクティブでない参加者の `deactivate` は、本家と同じくエラーです。
  - Mermaid 11 の参加者の種類 `participant A@{ "type": "database" }`。`actor` は人型で描きます。それ以外の種類（知らない種類を含む）は、本家のアイコンの代わりに、箱の中に `«database»` のように種類名を書きます。`alias` は表示名にし、ほかの属性は無視します。
- classDiagram: static（`$`、下線）と abstract（`*`、斜体）。メンバーの先頭の `~` は可視性（package）で、`List~int~` の対になった `~` は総称型です。
- stateDiagram: 並行領域（`--`）、複合状態の中の `direction`。

### テーマ

`theme` は default / neutral / dark / forest / base を選べます。`themeVariables` は primaryColor、primaryBorderColor、primaryTextColor、lineColor、secondaryColor、tertiaryColor、clusterBkg、clusterBorder、noteBkgColor、noteBorderColor などが効きます。
呼び出し側の `Options.Foreground` / `Background`（端末の色）がいちばん優先され、テーマはノードの塗りなどの配色を決めます。`Foreground` を指定したときは、themeVariables の textColor・primaryTextColor・lineColor は効きません（明るいページ向けの線の色が暗い端末で消えないように）。`style` や `classDef` で図の作者が指定した色はそのまま使います。ただし、テーマの文字色（`Foreground` を含む）が作者の指定した塗りと明るさで近すぎて読めない場合は、その塗りの上のラベルだけを黒か白に替えます（作者が `color:` で文字色を指定したときは替えません）。`dark` は、呼び出し側が色を指定しないときだけ暗い配色になります。

### 太字・斜体のフォント

内蔵フォントには Go Regular / Bold / Italic / Bold Italic を使います。外部フォントで太字や斜体を描き分けるには、書体ごとに Face を返す `Options.FontFunc` を渡します（`FaceFunc` や `Face` のときは、すべての書体が同じ Face で描かれます）。

```go
opt.FontFunc = func(px float64, st mermaid.FontStyle) (font.Face, error) {
    f := regular // st が mermaid.Bold なら太字のフォント、など
    return opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
}
```

### 本家との差

意図的に挙動を変えている箇所は、コード中の `NOTE(mermaid-compat)` にまとめてあります。主なもの:

- 描けないもの: flowchart の `icon` / `img` 形と、形の `fork` / `join` / `bang` / `cloud`、classDiagram の入れ子の namespace と lollipop インターフェース（`()--`）、sequenceDiagram の `par_over`、requirementDiagram の単独行の `名前:::クラス`（`class 名前 クラス` は使えます）、gantt の開始日のない最初のタスク（Mermaid は今日から始めますが、出力を決定的にするため描きません）と `vert` マーカー、`weekend`、`excludes` と一緒に使う `includes`、erDiagram の関係の端に書いた別名やクラス、gitGraph の `mainBranchName`（`main` 以外）（どれも `ErrUnsupported` になります）。mindmap のアイコンと CSS クラス、アニメーション（`animate`）は受け付けて無視します。gantt の「今日」マーカーも描きません（出力を決定的にするため）。
- `click`・`accTitle`・`accDescr`（複数行の `accDescr { … }` も含む）・リンクなど、操作やアクセシビリティの指定は画像に影響しません。mindmap と sankey は acc 指定を受け付けません。
- flowchart の `curve` は、直線系（linear、step 系）と曲線系（basis など）の 2 通りで描き分けます。
- stateDiagram の並行領域は、領域ごとに破線の枠で囲みます（本家は領域の間に破線を引きます）。
- 罫線出力: sequenceDiagram の `box` は背景を描きません。太字・斜体は区別しません。gitGraph は常に縦向きで、block-beta のエッジは図の下に一覧で示します。
- architecture-beta: 画像は本家と同じくエッジの辺（L/R/T/B）からサービスを格子に置きます（`a:R -- L:b` なら b は a の右）。エッジはサービスとグループのタイトルを避ける直交経路で引き、先に引いたエッジとはなるべく重ねません。経路探索には 1 回の描画あたりの歩数の上限（約 1 秒分）があり、超える大きな図（目安として、サービス 30 個・エッジ 100 本を超え、MaxWidth に合わせて配置をやり直す場合）や、探索の格子が 40000 点を超える図は `ErrTooLarge` になります。罫線出力に切り替えてください。罫線出力は、多いほうの向きに流れる階層レイアウトで近似します。アイコンは «database» のように名前で示し、エッジの `{group}` 修飾はサービスにつなぎます。
- quadrantChart・xychart-beta: 文字を回転できないので、縦軸のラベルとタイトルは横書きで置きます。
- C4: `UpdateRelStyle`・`UpdateLayoutConfig` などのスタイルとレイアウトの指定は効きません（`UpdateElementStyle` の色は効きます）。
- front matter で効くのは `title` と `config.theme` / `config.themeVariables` だけです（ソースの意味を変える `gitGraph.mainBranchName` は `ErrUnsupported` にします）。知らないテーマ名は default になります。

## 使い方

```go
img, err := mermaid.Render(ctx, src, mermaid.Options{
    MaxWidth:   800,             // 端末幅に収める (px)
    Scale:      1.5,             // 文字サイズ・線幅の倍率（1/16〜16 に丸める）
    Face:       face,            // nil なら内蔵の Go Regular（英字のみ）
    FaceFunc:   nil,             // 指定サイズの Face を返す関数。Face より優先され、文字を縮めて幅に収められる
    Foreground: color.White,     // nil なら背景に合わせて自動
    Background: nil,             // nil で透過。色を渡すと α は無視して不透明に塗る（color.Transparent は黒になる）
    MaxNodes:   500,             // 0 なら DefaultMaxNodes
    MaxPixels:  0,               // 縮小前の画像の画素数の上限。0 なら DefaultMaxPixels（4096×4096 = 64MiB）
    FontFunc:   nil,             // 書体（Regular/Bold/Italic/BoldItalic）ごとの Face。FaceFunc・Face より優先
})
var se *mermaid.SyntaxError
switch {
case errors.As(err, &se):            // 構文エラー（se.Line は元のソースの 1 始まりの行番号。se.Msg は約 200 バイトまで）
case errors.Is(err, mermaid.ErrUnsupported): // 未対応の図、またはフォントにない文字がラベルにある
case errors.Is(err, mermaid.ErrTooLarge):    // MaxNodes・MaxPixels などの上限を超えた、または MaxWidth に読める大きさで収まらない
case errors.Is(err, context.DeadlineExceeded):
default:                             // FaceFunc / FontFunc が返したエラー、内部エラー（"mermaid: internal error"）
}

typ, err := mermaid.Detect(src)                  // 描画せずに図の種類だけ判定
lines, err := mermaid.RenderText(ctx, src, mermaid.TextOptions{MaxCols: 80}) // 罫線文字。全角は 2 桁として数える
// キャッシュのキー: ソース・出力に効くオプション・Version をすべて含める
// 区切りのない連結は src の末尾とオプションの境目があいまいになるので、%q などで区切る。
// 色は %v ではなく RGBA() の値で書く（color.RGBA と color.NRGBA が同じ "{128 0 0 128}" になるが、意味は違う）
key := sha256(fmt.Sprintf("%q|%d|%v|%v|%v|%d|%d|%q|%s", src, opt.MaxWidth, opt.Scale, rgba(opt.Foreground), rgba(opt.Background), opt.MaxNodes, opt.MaxPixels, フォントの識別子, mermaid.Version))
// rgba(c) は c が nil なら "nil"、それ以外は fmt.Sprint(c.RGBA()) を返す
```

`Version` は、このライブラリのコードが変わって出力が変わるときに上がります。画素は `golang.org/x/image`（`vector` のラスタライズと縮小）にも依存するので、入力の結合文字の合成は `golang.org/x/text` の Unicode の表にも依存します。呼び出し側のビルドでこれらの版が変わりうるなら、その版（または呼び出し側自身のビルドの版）もキーに含めてください。

### MaxWidth に収める方法

文字を潰さないように、次の順に試します。

1. 要素の間隔を詰める
2. ラベルを折り返す幅を段階的に狭める
3. 内蔵フォントか `FaceFunc` の場合は、文字サイズを下げてレイアウトし直す（約 5px まで）
4. それでも収まらなければ画像全体を縮小する。ただし縮小後の文字が約 5px を下回る場合（縮小が 10% 未満のときを除く）は、読めない画像を返す代わりに `ErrTooLarge` を返す。固定の `Face` は大きさを変えられないので、3 を飛ばしてここに来る

幅には収まっても画素数が `MaxPixels` を超える図にも、1〜3 を同じように試します（`MaxWidth` が 0 のときも）。それでも超えれば `ErrTooLarge` です。

内蔵の Go Regular はラテン・ギリシャ・キリル文字しか持ちません。フォントにない文字（日本語など）がラベルにあると、`Render` は空の四角（豆腐）を描く代わりに `ErrUnsupported` を返します。日本語を描くには日本語フォントを渡してください。文字が潰れないよう `Face` ではなく `FaceFunc` を使います。

```go
f, _ := opentype.Parse(fontBytes)
opt.FaceFunc = func(px float64) (font.Face, error) {
    return opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
}
```

### 注意

- `MaxPixels` は画像を確保する前に確かめます。長いシーケンス図などは、既定では `ErrTooLarge` になります（RGBA は 1 画素 4 バイトなので、上限を上げるとその分メモリを使います）。
- `font.Face` は並行に使えません。並行に `Render` を呼ぶ場合は、呼び出しごとに別の Face を渡すか、呼び出すたびに新しい Face を返す `FaceFunc` を使ってください。
- `ErrTooLarge` は、要素数の上限を超えたときのほか、入れ子の深さ（subgraph・状態・ブロックなど）・block-beta の列数・gantt の期間といったこのライブラリの上限を超えたとき、MaxWidth に読める大きさで収まらないときにも返します。これらは Mermaid として正しい図なので、構文エラーにはしません。gvmd では `RenderText` かソースの表示に切り替えてください。
- `MaxNodes` と `MaxPixels` のほかに、変更できない内部の上限があり、超えると `ErrTooLarge` を返します。入力は 1 MiB まで、階層レイアウトの頂点（長いエッジの中継点を含む）は 20000 個まで、`RenderText` の文字セルは約 100 万個まで（200 桁なら約 5000 行）、経路探索は 1 回の呼び出しで約 840 万歩まで、入れ子は図の種類ごとに 16〜64 段までです。画像の高さには `MaxPixels`（面積）のほかに上限がないので、縦に長い図は幅が細いまま通ります。
- `MaxNodes`・`MaxPixels`・入力長の上限は、メモリと要素数を抑えるもので、描画時間の上限ではありません。上限の範囲内でも、縦に長い大きな図（関係の多い requirementDiagram など）の描画には数秒かかることがあります。画面を固めたくない呼び出し側は、必ず期限付きの `ctx`（`context.WithTimeout`）を渡してください。期限を過ぎると `context.DeadlineExceeded` を返します。ただし最後の画像の縮小（`MaxWidth` に合わせるビットマップ縮小）は途中で止められないので、期限を過ぎてから返るまでに、既定の `MaxPixels` で 100〜200ms ほどかかることがあります（縮小は期限を過ぎていれば始めません）。期限は、この遅れの分だけ余裕を持たせてください。
- `MaxNodes` は、ノード・エッジ・メッセージ・参加者・クラス・状態など、図の要素の数に、種類ごとに適用されます（`classDef` / `style` 行、gantt や timeline の `section`、ラベルの行数は数えません。これらは入力長の上限で抑えます）（合計ではありません。クラスのメンバーと ER の属性はクラス・エンティティごと）。そのため要素の総数は MaxNodes の数倍になりえます。作業量そのものは `MaxPixels`・経路探索の上限・`ctx` で抑えます。`class` 行や `Note` 行のように暗黙に作られる要素も数えます。
- `RenderText` のグラフ型の図（flowchart など）は、エッジの経路探索の量に上限があります（1 回の呼び出しで約 840 万歩）。超えると `ErrTooLarge` を返すので、多数のエッジが交差する大きな図でも 1 秒前後で返ります。そうした図は画像のほうが向いています。
- sequenceDiagram の `autonumber` は、開始番号と増分（`autonumber 10 5`）と `autonumber off` に対応します。罫線出力では、番号をメッセージの前に `10. ` のように書きます。
- `RenderText` が返す各行は、改行文字や制御文字（端末のエスケープシーケンスになるもの）を含まず、maxCols 桁以内であることを保証します。収まらない場合は `ErrTooLarge` を返します。桁数は Unicode の East Asian Width に従い、Wide/Fullwidth の文字（日本語・絵文字など）を 2 桁と数えます。
- ラベル中の制御文字は、そのまま書いても実体参照（`#27;` など）で書いても空白に置き換えます。`RenderText` の出力では、bidi の書式制御文字（U+202A〜U+202E、U+2066〜U+2069 など）も空白にします。先頭の BOM は無視します。異体字セレクタ（絵文字の表示を選ぶ U+FE0F など）は取り除き、元の文字だけを描きます。結合文字の付いた文字（macOS から貼り付けた NFD の `e` + U+0301 など）は、合成済みの文字（`é`）にしてから描きます（CJK 互換漢字など、結合文字の付かない文字はそのままです）。
- 出力は決定的です。同じ入力・オプション・`Version` からは同じ画素が出ます。CPU（amd64 / arm64）が違っても同じになるよう作っています。このライブラリのコードは積和の融合（FMA）を止めてあり、`TestNoFMA` が確かめます。ただし標準ライブラリの `math`（三角関数・対数など）は、arm64 では積和の融合や純 Go 実装との違いで最下位ビットが変わることがあり、座標を 1/64 px に丸める境界をまたぐと画素が変わりえます。arm64 の実機での golden の確認もしていないので、CPU をまたいだ一致は保証しません（同じ CPU・同じビルドでは決定的です。キャッシュを端末ごとに持つ gvmd には影響しません）。ただし `Face` / `FaceFunc` で渡したフォントの描画は、その Face の実装によります。`opentype.Face` のグリフは x/image の中で描かれるので、CPU によって画素が変わることがあります。gantt の「今日」マーカーは描きません。gitGraph の自動 ID は乱数ではなく連番から作ります。
- Mermaid 本家と意図的に挙動を変えている箇所は、コード中の `NOTE(mermaid-compat)` にまとめてあります。

## 開発

```sh
go test ./...                          # golden・決定性・幅・エラー・依存のテスト
go test -update -run 'TestGolden' .    # golden を作り直す（差分は必ず目視で確認）。出力が変わったら先に Version を上げる
go test -run '^$' -fuzz FuzzRender .   # Fuzz テスト
GOAMD64=v3 go test . && go test -tags noasm .   # FMA の融合と vector の純 Go 実装でも golden と一致するか（arm64 との一致の確認）
go run ./cmd/mermaidpng --width 600 < testdata/flowchart_basic.mmd > out.png
go run ./cmd/mermaidpng --text 80 < testdata/sequence_basic.mmd
go run ./cmd/mermaidpng --out auto --width 800 < testdata/flowchart_basic.mmd   # 端末に直接表示（sixel / iterm2 / kitty / auto）
# 端末出力で --width を省くと 1000px。tmux の中では kitty / iterm2 を passthrough で包む（tmux 側で set -g allow-passthrough on が必要）。
# 大きな iterm2 画像は tmux の中では multipart 形式（iTerm2 3.5 以降）で分割する。sixel は tmux 3.4 以降がそのまま扱う。
# tmux の中の kitty は直接配置なので、tmux の再描画で消えたりずれたりすることがある
go run ./cmd/mermaidpng --font /path/to/NotoSansJP.ttf --dark < diagram.mmd > out.png
```

- コーディング規約: [docs/CODING_GUIDELINES.md](docs/CODING_GUIDELINES.md)
- 実装プラン: [docs/plans/mermaid-renderer.md](docs/plans/mermaid-renderer.md)
