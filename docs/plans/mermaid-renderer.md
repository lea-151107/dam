# Mermaid → image.Image ライブラリ（pure Go）実装計画

## Context
`docs/RFP.txt` の要件に基づき、gvmd（端末向け Markdown ビューア）が Mermaid コードブロックを画像として表示するためのライブラリを作る。
gvmd は単一バイナリ配布のため **cgo・外部コマンド・ネットワーク禁止**。出力は `image.Image`(RGBA) で、端末エンコードは gvmd 側。
対応図種（ユーザー確定）: flowchart/graph, sequenceDiagram, classDiagram, stateDiagram, pie, gantt, gitGraph, mindmap。
依存は **標準ライブラリ + golang.org/x/image のみ**。配置は `/home/lea/dam` 直下（現在 `docs/` のみ、コードなし）。
モジュールパスは仮に `github.com/lea-151107/dam` とし、実装開始時に差し替え可能にする。

## 公開 API（ルートパッケージ `mermaid`）
RFP の API イメージに準拠し、以下を追加:
```go
const Version = "0.1.0"
type DiagramType int // Flowchart, Sequence, Class, State, Pie, Gantt, GitGraph, Mindmap
type Options struct {
    MaxWidth   int; Scale float64; Face font.Face
    Foreground color.Color; Background color.Color // nil=透過
    MaxNodes   int // 0 で既定値 (例 500)
}
func Detect(src string) (DiagramType, error)
func Render(ctx context.Context, src string, opt Options) (image.Image, error)   // *image.RGBA を返す
func RenderText(ctx context.Context, src string, maxCols int) ([]string, error)
var ErrUnsupported, ErrTooLarge   // ErrTooLarge = ノード/エッジ数上限超過
type SyntaxError struct{ Line int; Msg string } // Error(): "mermaid: line N: msg"
```
- キャンセル時は `ctx.Err()` をそのまま返す（errors.Is で判別可）。
- Detect: front matter `---…---`、`%%{init}%%`、`%%` コメント、空行を飛ばして先頭キーワードで判定。erDiagram/journey/timeline 等は ErrUnsupported。

## ディレクトリ構成
```
go.mod                      // require golang.org/x/image のみ
mermaid.go detect.go errors.go options.go version.go
internal/source/            // 行分割・コメント除去・行番号付きトークナイザ（全パーサ共通）
internal/diagram/{flowchart,sequence,class,state,pie,gantt,gitgraph,mindmap}/
                            // 各: parse.go(AST) + layout.go(→scene)
internal/layout/sugiyama/   // 階層レイアウト（flowchart/class/state で共用）
internal/layout/tree/       // mindmap 用ツリーレイアウト
internal/scene/             // 描画命令（Rect/RoundRect/Ellipse/Polygon/Path/Text, stroke/fill/dash/arrow）
internal/raster/            // scene → *image.RGBA（x/image/vector + 自前ストローカ）
internal/textmeasure/       // font.Face での計測・折返し・<br> 対応
internal/theme/             // Fg/Bg からパレット導出（ノード塗り= Fg を低αで、パイ配色は固定色相列）
internal/textgrid/          // RenderText 用：罫線文字グリッド＋東アジア幅テーブル
cmd/mermaidpng/             // 手動確認用 CLI（stdin → PNG）
testdata/*.mmd, testdata/golden/*.png
```

## 主要設計
1. **パイプライン**: parse → AST → layout（ノードサイズは textmeasure で計測）→ `scene.Scene`（px@Scale=1 の浮動小数）→ raster。
2. **フォント**: `Face==nil` なら `x/image/font/gofont/goregular` を `opentype.NewFace` で Scale に応じたサイズで生成。外部 Face はそのまま使い、Scale はレイアウト（余白・線幅）に適用。
3. **MaxWidth への収め方**（文字を潰さない順に段階適用）:
   (a) 間隔（nodesep/ranksep/padding）を縮める → (b) ラベルを折り返し幅を狭めて再レイアウト → (c) 最終手段として全体を一様縮小（内蔵フォント時はフォントサイズを下げて再レイアウト、外部 Face 時のみ x/image/draw.CatmullRom で縮小）。
4. **ラスタライズ**: `vector.Rasterizer` で塗り。線は自前ストローカで多角形化（miter/round join、破線、矢じり・○・× マーカー）。曲線は 2/3 次ベジェ（vector が直接対応）。テキストは `font.Drawer` で描画。
5. **決定性**: map 反復を使わず挿入順スライスで管理、ソートは安定ソート＋明示タイブレーク、乱数・現在時刻不使用（gantt の today マーカーは出さない）。アーキ差（arm64 の FMA 融合）対策としてレイアウト座標を 1/64px 固定小数に丸めてから raster へ渡す。
6. **異常入力対策**: パース時にノード・エッジ・メッセージ数を数え MaxNodes 超で ErrTooLarge。Sugiyama の交差削減反復・各ループで `ctx.Err()` を定期チェック。入力長上限（例 1MB）も設ける。
7. **Sugiyama**: DFS による閉路除去 → longest-path 階層化（+簡易 promotion）→ ダミーノード挿入 → barycenter 交差削減（上下スイープ、反復上限）→ Brandes–Köpf 座標割当 → TD/LR/BT/RL は座標変換で対応。subgraph / 複合 state はクラスタとして同一層範囲に詰め、外枠を後付け。エッジはダミー点を通る折れ線をベジェ平滑化。
8. **各図種のスコープ（v0.1）**
   - flowchart: 形状 `[] () ([]) [[]] [()] (()) {} {{}} >] [/ /] [\ \]`、エッジ `--> --- -.-> ==> --o --x <-->`、`-- text -->`/`|text|`、`A & B`、チェーン、subgraph、classDef/style の fill/stroke/color のみ。
   - sequence: participant/actor/alias、`->> -> -->> --> -x -) `、note left/right/over、loop/alt/else/opt/par/critical/break、activate(+/-)、autonumber。
   - class: class 定義・メンバ・ジェネリクス `~T~`、関係 `<|-- *-- o-- --> ..> ..|>`、多重度ラベル、注釈 `<<interface>>`。
   - state: `[*]`、遷移+ラベル、複合状態、choice/fork/join、note。
   - pie: title、showData、ラベル付き凡例。
   - gantt: dateFormat（YYYY-MM-DD 系サブセット）、section、タスク（id, after, 期間 d/w/h, done/active/crit/milestone）、axisFormat 簡易。
   - gitGraph: commit(id/tag/type)、branch、checkout/switch、merge、cherry-pick。レーン配置は単純。
   - mindmap: インデント階層、形状 `[] () (()) ))(( {{}}`、tree レイアウト（左右振り分けの水平ツリー）。
9. **RenderText**: 罫線文字（─│┌┐└┘├┤┬┴┼▶▼）で描くグリッド。v0.1 は flowchart・sequence・pie(横棒)・gantt を対応、他は ErrUnsupported。全角文字は幅2で扱う。maxCols 超過時はラベル折返し→間隔縮小→不可なら ErrTooLarge。

## 実装順
0. **準備（コードを書く前に行う）**
   - このプランを `docs/plans/mermaid-renderer.md` として保存する。
   - コーディング規約 `docs/CODING_GUIDELINES.md` を作る（日本語）。内容の案は下の「コーディング規約（案）」。完成したらユーザーに確認してもらい、OK が出てから手順 1 に進む。
1. 基盤: go.mod, API, errors, Detect, source, scene, raster, textmeasure, theme, cmd/mermaidpng
2. sugiyama + flowchart（最重要・最大工数）
3. sequence
4. class, state（sugiyama 流用）
5. pie, gantt
6. gitGraph, mindmap
7. RenderText

## コーディング規約（案）→ docs/CODING_GUIDELINES.md
1. **制約**: 使ってよいのは標準ライブラリと golang.org/x/image だけ。cgo・`os/exec`・`net`・`unsafe` は禁止。`init()` での副作用も禁止。対応する Go のバージョンは go.mod に書く。
2. **整形と静的解析**: `gofmt`/`goimports` を通し、`go vet` で警告がないこと。staticcheck は任意（依存に加えず、ツールとしてだけ使う）。
3. **パッケージ構成**: 公開 API はルートパッケージだけに置き、それ以外は `internal/` に入れる。依存の向きは 公開API → diagram → layout/scene → raster/textmeasure の一方向とし、循環させない。ファイル名はスネークケースにせず、短い小文字にする。
4. **命名**: Go の慣習（MixedCaps、頭字語は大文字で `ID`/`URL`）に従う。パッケージ名は単数形で短くし、`util`・`common` は禁止。
5. **エラー**: 番兵エラーは `ErrXxx`。構文エラーは必ず `*SyntaxError{Line}` にし、行番号は 1 から数える。ラップは `fmt.Errorf("...: %w", err)`。メッセージは小文字で始め、末尾に句点を付けない。ライブラリでは panic しない。内部で起きた panic は Render 境界で recover してエラーに変換する。
6. **決定性**: map の反復結果を出力順に使わない（キーをソートするか、挿入順のスライスを使う）。`sort.SliceStable` と明示的なタイブレークを使う。`math/rand`・`time.Now`・goroutine の完了順に依存しない。座標は raster に渡す前に 1/64px に丸める。
7. **リソース保護**: ループが入力サイズに比例しうる箇所では `ctx.Err()` を確認する（N 回に 1 回程度でよい）。反復回数とサイズには上限定数を設け、名前を付けて定義する。
8. **数値と単位**: レイアウトは float64 で扱い、単位は「px @ Scale=1」。変数名やコメントで単位を明示する。マジックナンバーは名前付き定数にする。
9. **コメントとドキュメント**: 公開識別子には godoc コメントを必ず付ける（英語）。内部コメントは日本語でも英語でもよいが、「何をするか」ではなく「なぜそうするか」を書く。
10. **テスト**: テーブル駆動にし、`t.Run` でサブテストに名前を付ける。golden ファイルは `testdata/golden/`、更新は `-update` フラグで行う。パーサには Fuzz テストを付ける。新しい構文に対応したら、golden と構文エラーのケースを最低 1 つずつ追加する。
11. **コミット**: 1 コミット 1 論理変更。メッセージは `<pkg>: <要約>` の形式（例 `flowchart: support subgraph`）。

## 検証
- `CGO_ENABLED=0 go build ./... && go vet ./...`
- `go test ./...`:
  - golden テスト: `testdata/*.mmd` を描画し PNG と比較（`-update` で再生成）。図種ごと複数ケース、ライト/ダーク/透過、MaxWidth 制約あり/なし。
  - 決定性テスト: 同一入力を2回（別 goroutine 含む）描画しピクセル sha256 一致を確認。
  - 出力幅テスト: `img.Bounds().Dx() <= MaxWidth` を全 golden で確認。
  - エラーテスト: 未対応図→`errors.Is(err, ErrUnsupported)`、構文エラー→`*SyntaxError` の Line が正しい、MaxNodes 超→ErrTooLarge、`context.WithTimeout` 極短→`context.DeadlineExceeded`。
  - 依存テスト: `go list -deps` 相当で `os/exec`, `net`, `runtime/cgo`, `unsafe` 経由の C 依存が無いことを確認。
  - Fuzz: 各パーサに `FuzzParse`（panic しない・時間内に返る）。
- 手動: `go run ./cmd/mermaidpng < testdata/flowchart_basic.mmd > /tmp/out.png` で目視確認（日本語は外部 Face を渡すフラグ `--font path.ttf` で確認）。

## 実装状況（2026-11-03）
- 手順 0〜7 をすべて実装した。`go test ./...` は全件成功。Fuzz（`FuzzRender`）は 420 万回実行して、panic も 2 秒を超える入力もなかった。
- プランから変えた点:
  - クラスタ（subgraph・複合状態・namespace）は、層ごとの並べ替えでは他のノードと重なったため、クラスタを再帰的にレイアウトして 1 つの大きなノードとして親に置く「複合レイアウト」に変更した（`internal/layout/sugiyama/compound.go`）。
  - x/image が `golang.org/x/text` と `golang.org/x/sys/cpu` を間接的に使っていたので、依存テストで許可した（どちらも pure Go）。
  - Fuzz テストは図種ごとの `FuzzParse` ではなく、ルートの `FuzzRender` で全パーサとレイアウトを通している。
  - golden 画像は内蔵フォントだけで作っている。日本語は外部フォント（`cmd/mermaidpng --font`）で目視確認した。テスト用の CJK フォントは `testdata/` に置いていない。
- 既知の制限:
  - （v0.1.5 で解消）RenderText はクラスタをまたぐエッジがノードの枠を横切ることがあった。
  - アーキテクチャ間の決定性は、座標を 1/64px に丸めることで差を抑えているが、x/image/vector のアセンブリ実装（amd64）と純 Go 実装の差までは検証していない。

## レビュー後の修正（v0.1.1）
adversarial-review で見つかった不具合を修正した。修正した不具合ごとに `regress_test.go` へ回帰テストを追加している。
- MaxNodes: 要素を作る共通の関数（flowchart の `node`、class の `class`、sequence の `participant`、state の `state`）で数えるようにした。flowchart のレイアウトで2乗の計算量になっていた処理（輪郭計算・`firstMember`）は、事前計算で1回にした。
- キャンセル: ctx の確認間隔を 16 回に 1 回に縮め、ガントチャートの日ごとのループでも確認するようにした。除外日の判定は日数の整数で行うようにした。
- MaxWidth: `Options.FaceFunc` を追加し、外部フォントでも文字サイズを下げて収められるようにした。ガントチャートは右にはみ出した分だけ図の幅を縮めてレイアウトし直す。
- RenderText: 出口で、全行に改行文字がなく maxCols 以内であることを検査する。各図種のタイトルは折り返しまたは切り詰めを行い、シーケンス図のキャンバス幅は実際に描く範囲から計算する。
- flowchart: 自分を含むサブグラフへのエッジを、ノードから枠までの線として描く。
- 軽微な修正:
  - ガントチャートの `excludes` のエラー行番号
  - 中身が空の複合状態を入れ子にしたときの二重描画
  - `textgrid.Wrap` の無限ループ
  - 全角文字の2セル目が上書きされたときのずれ
  - エラーメッセージの重複
  - 約 292 年を超える期間でのバーの位置

## 2回目のレビュー後の修正（v0.1.2）
サブエージェントによる再レビューで見つかった5件を修正した。回帰テストは `regress_test.go` に追加している。
- 文字幅: `textgrid.RuneWidth` の手書きの範囲表を、`golang.org/x/text/width`（Unicode の East Asian Width）から生成した表 `internal/textgrid/widthtable.go` に置き換えた。生成は `go generate ./internal/textgrid`（`gen_width.go`、`//go:build ignore`）で行う。x/text は生成時にだけ使い、ライブラリ本体は x/text を import しない。これで絵文字（✅ 🚀 など）が2桁として数えられる。
- 制御文字: `source.Split` で生の制御文字を、`source.Label` で実体参照（`#13;`、`#27;` など）から展開された制御文字を、それぞれ空白に置き換える。U+2028・U+2029 も同じ扱い。`RenderText` の出口でも `textgrid.Sanitize` をかけて、端末のエスケープシーケンスが出力に入らないことを保証する。
- BOM: `source.Split` の先頭で UTF-8 の BOM を取り除く。
- RenderText: 罫線出力に対応しているかどうかを構文解析の前に判定する。対応していない図は、本文が壊れていても大きすぎても `ErrUnsupported` を返す。

## 3回目のレビュー後の修正（v0.1.3）
サブエージェントによるプロジェクト全体のレビューを受けて修正した。
- MaxWidth: 文字の縮小は約 5px（`minTextPx`）までにした。画像の縮小は、縮小後の文字が 5px 以上か縮小が 10% 未満のときだけ行う。それ以外は `ErrTooLarge` を返す。以前は縮小に下限がなく、300×6px のような読めない画像を返していた。文字の縮小ループでも ctx を確認する。
- 閉じていないブロック（subgraph、loop など、class 本体、namespace、複合状態）のエラー行を、4 図種とも開いた行に揃えた。
- gantt: `2000-13-45` のような日付の形をした不正な開始日を、タスク ID ではなく不正な日付として報告する。
- CLI:
  - 端末出力で `--width` を省いたときは 1000px にする。
  - `--timeout` をエンコードまで含めて効かせる。
  - sixel の減色は 6×6×6 の色立方体から直接求める。帯ごとの処理は画素数に比例するようにした（巨大な画像で 1.1 秒 → 0.15 秒）。
  - kitty は全チャンクに `q=2` を付ける。
  - tmux の中（`TMUX` が設定されている）では、kitty と iTerm2 のシーケンスを passthrough で包む。

## 4回目のレビュー後の修正（v0.1.4）
- 固定の `Face` の文字サイズ（em）の推定を、行の高さ ÷ 1.2 から cap height ÷ 0.7 に変えた。CJK フォントで 21% 過大に見積もり、実効の下限が約 4px になっていた。cap height がない Face だけ、従来の推定を使う。
- CLI: `--fontsize` を `Scale` に換算するようにした。以前は FaceFunc が要求と違う大きさの face を返していたので、ライブラリの可読性ルールが実際の文字サイズで効いていなかった（`--fontsize 6` で約 2px の文字が出ていた）。
- CLI: tmux の中で 512KiB を超える iTerm2 画像は multipart 形式（MultipartFile / FilePart / FileEnd）に分割する。tmux は 1MiB を超える passthrough シーケンスを捨てる。
- `ErrTooLarge` の godoc と README に、MaxWidth に読める大きさで収まらない場合も含むことを書いた。

## flowchart の罫線出力の作り直し（v0.1.5）
戻る矢印や分岐があると、罫線出力が崩れていた。具体的には、矢印と線が重なる（`▲┬`、`▼` の下に `┴`）、平行な線が同じ行で混ざる、はい / いいえ のラベルが線から離れる、という崩れ方だった。原因は、レイアウトの折れ線を中間点で Z 字につないでいたことにある。そのため、全部のエッジが箱の辺の中央に集まり、隣り合うランクの間の横線がすべて同じ行に乗っていた。

`internal/diagram/flowchart/route.go` で、文字グリッドの上でエッジを経路探索するように変えた。
- 箱の位置だけをレイアウトから使う。エッジは Dijkstra 法（バケットキュー＋A*）で直交経路を探す。コストは、曲がり、交差、箱やほかの線への接近、レイアウトの折れ線からの距離、ランクの間の中央からの距離で決める。
- 端点は、エッジごとに箱の辺の別のセルを使う。線どうしは同じセルを並んで通らない（直角に交差するだけ）。曲がり角と、矢印やその直後のセルは、ほかの線が通れない。
- 矢印は箱の手前のセルに置き、線を枠につながない。そのため、矢印の先の枠に分岐記号が出ない。
- 同じ箱に同じ印で入る線は、矢印の手前で合流できる。同じ箱から出る線は、共有の線から分岐できる。ただし、ラベルが付いた線で共有できるのは、ラベルより先の部分だけにした。また、1 本の線の上では、分岐を合流より前に置く。こうしないと、実際にはない経路が描かれてしまう。
- ラベルはそのエッジ自身の線の上に置く。横の区間なら線の上に重ねて書き、縦の区間なら線を横切って書く。どちらもできないときは線の横に置く。ラベルどうしは同じ行で接しないようにし、ほかの線にも接しないようにする。
- 箱の大きさは、出入りするエッジの数に合わせて広げる。経路が見つからないときは、重なりを許して描く。エッジを黙って消すことはしない。
- 回帰テストは `regress_test.go` の TestFlowchartTextRouting。矢印の後ろに線があること、矢印の先が分岐記号でないこと、ラベルが重複していないことと接していないことを確かめる。
- 性能: 150 ノード・300 エッジで約 0.3 秒。ctx の期限には 50ms で応じる。
- 自己ループのラベル（v0.1.5 のうちに修正）: ラベル付きの自己ループで、ラベルがループ自身の線を上書きしていた（`┌x─┐`、LR では `retry` が角と上の線を覆う）。原因は 2 つあった。①ラベルを置く時点ではそのエッジの線がまだ記録されておらず、空きセルに見えていた。②線の横に置けないときの最終手段が、レイアウトの位置にそのまま書いていた。修正後は、ラベルは自分の線のほかの部分を覆わない。横線の上に書くときは、角の隣に線を 2 セル残す。自分の矢印にも接しない。自己ループのラベルはループの外側に置く。最終手段は、線の近くの空きを外側へ探す。回帰テストは `TestSelfLoopLabel` で、ラベルの有無で線の文字数が変わらないことを確かめる。

## 土台の整備（v0.3.0）
「dam を完全にする計画」のフェーズ 0。後のフェーズで図種を増やす前に、土台と品質保証を直した。
- 図種レジストリ: 図種を足すたびに 4 か所（`keywordType`、`typeNames`、`parse`、`hasText`）を手で同期していたのを、`registry.go` の表 1 つにまとめた。`TestRegistry` が表の並び順、キーワードの重複、全図種に検査用の入力（`registrySamples`）があること、`text` フィールドと実装が一致することを確かめる。`FuzzRender` の種も `registrySamples` から作る。
- アーキテクチャ間の決定性: `GOAMD64=v3`（FMA の融合）で golden 14 件中 7 件の画素が変わった（各 1〜12 画素、色の差は 255 段階中 1）。規約にあった「1/64 px に丸めれば吸収できる」は誤りだった。文字を描かない face で切り分けた結果、原因はグリフの曲線を x/image/vector が float32 で分割していることだった（図形は一致していた）。
  - `internal/curve`: 曲線を 26.6 固定小数の整数演算だけで折れ線にする。
  - `internal/outline`: 内蔵フォントの Face。sfnt の輪郭を `curve` で折れ線にして描く。グリフの見た目の差はアンチエイリアスの範囲（255 段階中 25 以下）。
  - dal の数式フォントのグリフにも同じ問題があり（golden 43 件中 17 件）、同じ `curve` を使うように直した（dal v0.1.5）。
  - `GOAMD64=v1/v3` と `-tags noasm` の 4 通りすべてで golden が一致することを確かめた。
- 日本語の golden: Noto Sans CJK JP のサブセット（53KB、OFL）を `testdata/fonts/` に置き、`*_ja.mmd`（flowchart、sequence、gantt）を `outline` の Face で描く。
- 図種ごとの単体テスト: 8 図種すべてに `parse_test.go` を置いた。AST の要約と、構文エラーの行番号を確かめる。
- golden と Version の検査を、ファイルごとのハッシュに変えた。既存の golden が変わったら Version を上げないと失敗する。golden の追加と削除だけなら Version を上げなくてよい。

## 本家との差を埋める（v0.4.0）
「dam を完全にする計画」のフェーズ 1。
- front matter と `%%{init}%%`: `source.SplitDoc` が行と一緒に title とテーマ設定を返す。YAML は入れ子の対応だけ、ディレクティブは JSON5 の一部（クォートなしのキー、単一引用符、末尾のカンマ、値のないキー `%%{wrap}%%`）を読む。壊れたディレクティブは構文エラー。
- タイトル: front matter の title を全図種で描く（`diagram.AddTitle`）。pie / gantt は自前のタイトルとして受け取り、sequence の `title` 文は front matter より優先する。罫線出力でも先頭に出す。
- テーマ: `theme.NewWith` が default / neutral / dark / forest / base と themeVariables を扱う。呼び出し側の色が最優先。CSS の色名 148 個すべてに対応した（以前は一部だけで、`box Aqua` などが効かなかった）。
- 複数行の Markdown 文字列: `` "` `` が閉じるまで行をつなぐ。行番号は開いた行。
- Mermaid 11 の flowchart: `A@{ shape, label }`（45 種類の形と別名。30 種類の形を追加）、エッジ ID と `e1@{ curve }`。`icon` / `img` は描けないので構文エラー。
- 太字・斜体・下線: `internal/richtext` がラベルの中に書体の印（U+10FFF0〜）を埋め込む。計測・描画は区間ごとに書体の face を使い、罫線出力は印を幅 0 として取り除く。内蔵フォントは Go の 4 書体、外部フォントは `Options.FontFunc`。class の static / abstract も下線 / 斜体にした。
- sequence: box（色付き）、create / destroy、罫線出力のアクティベーション（`┃`）。
- サブグラフごとの direction（中のノードが外とつながっていないときだけ）、state の並行領域、複合状態の中の direction（以前は図全体の向きを変えていた）。
- ついでに直した不具合: HTML の数値参照 `&#35;` が `&#` と読まれていた。サブグラフで終わるエッジの矢印が、終点と同じ位置の点が残って常に下を向いていた。

## 罫線出力を全図種に（v0.5.0）
「dam を完全にする計画」のフェーズ 2。
- `internal/textgraph`: flowchart の罫線出力（文字グリッド上の経路探索を含む）を、ノード・エッジ・クラスタの汎用モデルで描くパッケージに切り出した。移した時点で flowchart の罫線出力の golden は 1 文字も変わらなかった。
- 汎用化で足したもの: 区画付きの箱（class のメンバー）、1 文字のノード（● ◉ ◇ ━。頭は記号の隣に置く）、印ごとの頭（△ ◆ ◇ ○ ×）、破線と太線（直線のセルだけ ┄┆ / ━┃ に置き換えるので分岐は残る）、クラスタの枠で止まるエッジ（複合状態、サブグラフへの参照）、クラスタごとの向き。
- クラスタの余白を 1 から 2 にした。箱が枠に接していると、枠側の辺から線を出せず遠回りしていた。
- class・state は textgraph で、gitGraph はブランチごとの縦の列で、mindmap は ├─ └─ の木で描く。

## 新しい図種（v0.6.0）
「dam を完全にする計画」のフェーズ 3。14 図種を追加し、Mermaid の図種のうち zenuml（本家でも外部プラグイン）以外にすべて対応した。
- `internal/boxgraph`: 箱とエッジの図の画像レイアウト（区画は列を揃えた表）と、同じモデルから textgraph への変換。erDiagram・requirementDiagram・C4・architecture-beta はモデルを作るだけで画像と罫線出力の両方が得られる。ER の鳥の足は scene の印として追加し、罫線出力では多重度の文字にする。
- `internal/chart`: 目盛りの刻み（1-2-5 系列。刻みの小数桁で丸めて 0.6000000000000001 を防ぐ）と数値の書式。xychart と radar で使う。
- 独自のレイアウト: timeline（期間の列と出来事）、journey（点数の高さの表情）、quadrantChart、xychart-beta（縦・横、棒と折れ線）、kanban、block-beta（入れ子の格子、形、ブロック矢印）、packet-beta（32 ビットの行、行をまたぐフィールドの分割）、sankey-beta（最長経路で列を決め、出口は右端。循環はエラー）、radar-beta、treemap-beta（squarified）。
- architecture-beta の配置は近似（エッジの辺の多数決で LR か TB にし、各エッジの向きを辺に合わせる）。README の「本家との差」に書いた。
- 各図種に、画像と罫線出力の golden、パーサの単体テスト（行番号付きの構文エラーを含む）、Fuzz の種（`registrySamples`）がある。

## フェーズ 3 の見直し（v0.6.1）
- fuzz で見つかった不具合: xychart で値がすべて 0 のとき罫線出力が panic、treemap で区分のラベルが空のとき panic。どちらも直し、fuzz が保存した入力は `testdata/fuzz/FuzzRender/` に回帰テストとして残した。
- 区切りのない長いラベルで罫線出力が遅かった（0.52 秒）。textgraph は、配置の時点で幅が maxCols を超える試行では経路探索を省く（0.04 秒。出力は同じ）。
- 値の範囲や合計が float64 で表せない入力（xychart の -1e308〜1e308、treemap・sankey の合計の桁あふれ）で、画像が終わらない、または panic していた。構文エラーにした（`TestHugeValues`）。
- kanban のメタデータを map の順で読んでいたため、知らないキーが複数あるとエラーの内容が実行ごとに変わり得た。キーの順に読む。

## フェーズ 0〜2 の adversarial-review（v0.6.2）
- 罫線出力で幅の足りない試行を飛ばす最適化（v0.6.1）が、配置の幅を出力の幅の下限とみなしていた。実際の出力は狭くなりうる（エッジのラベル用の領域が使われないことがある）ため、収まる試行まで飛ばし、`maxCols` が出力の幅ちょうどのとき別の出力になっていた（er・flowchart・state で確認）。下限として確かな「一番広い箱の幅」で判定するように直した。`TestTextAtOwnWidth` で検査する。長いラベル 1 つの入力の速さは、いったん元の約 0.5 秒に戻った。その後、ラベルの置き場所の判定（`placeLabel`）を、空いていないセルの 2 次元累積和で矩形ごとに一定時間で判定するように変え、約 0.19 秒にした（出力は同じ）。
- `%%{init}%%` で値を引用符で囲まないと（`curve: basis`）、図全体が構文エラーになっていた。ディレクティブでも引用符なしの語を値として読む（`TestLooseDirective`）。

## フェーズ 3 の adversarial-review（v0.6.3）
- 確認して崩れなかった前提: 新しい図種も MaxNodes を守る（上限 3 で全入力を確認）。本家ドキュメントの C4Deployment（入れ子のノード、`$tags` などの名前付き引数）と block-beta（`&nbsp;` の矢印、グループへのエッジ）がそのまま描ける。
- 崩れた前提: 「要素数の上限があれば大きさも抑えられる」。block-beta の入れ子は幅が掛け算で増え（64 列 × 15 段）、要素は 30 個でも幅が 64^15 になった。罫線出力はセル数の検査の掛け算があふれて panic し、画像は浮動小数点の幅を int に変換した時点で壊れて 1 画素幅の画像を黙って返した。
- 修正: raster は画素数を int に変換する前に float で検査する（NaN も弾く）。textgrid.New は負の大きさと各辺を先に検査する。textgraph はグリッドを作る前に配置の大きさを検査する。block の罫線出力は入れ子ごとに幅を打ち切る。`TestNestedBlockOverflow`。

## フェーズ 0 の adversarial-review（v0.6.4）
- 確認して崩れなかった前提: 内蔵フォントの Face を呼び出しごとに作っても遅くない（`BuiltinFaces` 4 書体で約 9µs。sfnt の解析は遅延する）。
- ほぼ崩れなかった前提: outline.Face と opentype.Face が同じ大きさ（ppem）で描く。数式は同じだが浮動小数点の演算順序が違い、丸めの境界の大きさで 1/64 ずれうる。opentype と同じ式に揃えた（golden は変わらず）。

## フェーズ 1 の adversarial-review（v0.6.5）
- 確認して崩れなかった前提: 書式の解釈が、タグでない `<i>` などを壊さない（class のジェネリクス `List~i~` は `List<i>` のまま描かれる）。
- 崩れた前提: 複数行の Markdown 文字列の連結は、それを使う図種にしか影響しない。行分割は全図種で共通のため、sequence のメッセージや gantt のタスク名に `` "` `` があると後続の行を飲み込み、図全体が構文エラーになっていた。本家で Markdown 文字列を使えるのは flowchart と mindmap だけなので、ヘッダーのキーワードがそれらのときだけ連結する。
- まとめてのレビュー（v0.6.2）で直した、引用符のないディレクティブの値もこのフェーズの不具合だった。

## フェーズ 2 の adversarial-review（v0.6.6）
- 確認して崩れなかった前提: 新しい罫線出力も「各行が maxCols 以内、制御文字なし」を守る（全入力を幅 20・40・80 で検査する既存の回帰テスト）。
- 崩れた前提: gitGraph の罫線出力で分岐の位置が親のコミットと一致する。分岐の線を子の 1 行上で曲げていたため、ブランチを作った後に元のブランチのコミットが続くと、後のコミットから分岐したように見えた。分岐は親の直後、合流は合流コミットの直前で曲げる（`TestGitGraphBranchOff`）。
- まとめてのレビュー（v0.6.2）で直した、罫線出力の試行を飛ばしすぎる問題もこのフェーズの共通化の範囲だった。

## architecture-beta の格子配置と golden の目視確認（v0.6.7）
- 計画では「辺の向き（L/R/T/B）から格子に配置する」としていたが、階層レイアウトで近似していた。画像は計画どおり格子に置く（`grid.go`）。`a:R -- L:b` なら b を a の右の格子に置き、ふさがっていれば同じ向きに進める。つながっていない部分は右に並べる。エッジは指定された辺から出て直交する折れ線で入り、グループの枠はメンバーを囲む（入れ子も可）。グループのタイトルはエッジの上に背景付きで描く。罫線出力は計画どおり textgraph。`TestPlace`。
- フェーズ 3 の golden 15 枚を縮小して並べて目で確認した（フェーズ 1・2 の golden は作成時に確認済み）。

## architecture-beta の格子配置のレビューと golden の個別確認（v0.6.8）
- フェーズ 3 の golden 15 枚を 1 枚ずつ開いて確認した。軽微な点: C4 で 2 階層をまたぐ曲線が箱の下をくぐる、radar のタイトルが凡例の分だけ中心からずれる。
- v0.6.7 の格子配置をレビューした。崩れた前提: 「グループの枠はメンバーだけを囲む」。枠をメンバーの外接矩形にしていたため、メンバーの間に置かれたグループ外のサービスまで囲み、所属を誤って示した。配置を階層化した: グループの中は直接の子（サービスと内側のグループ）を辺の向きで格子に置き、親から見るとグループ全体が 1 単位になる。枠にはそのグループの子孫しか入らない（`TestUnits`）。

## 計画の項目と完了の根拠（最終状態 v0.6.8）
| 計画の項目 | 根拠 |
|---|---|
| フェーズ 0: レジストリ / 日本語 golden / 図種ごとの単体テスト / アーキテクチャ間の決定性 / Fuzz の種 | `registry.go`・`TestRegistry`、`testdata/*_ja.mmd` と `testdata/fonts/`、`internal/diagram/*/parse_test.go`（22 図種すべて）、`GOAMD64=v1/v3 × asm/noasm` の 4 通りで golden 一致、`registrySamples` からの種 |
| フェーズ 1: SplitDoc / Mermaid 11 / タイトル / 太字・斜体 / sequence / direction / 並行領域 / init / README | 各節の記録と golden（flowchart_shapes・styles・direction・theme、class_classifiers、sequence_lifecycle、state_concurrent） |
| フェーズ 2: textgraph と 4 図種の罫線出力 | 全 22 図種の罫線出力の golden（`.txt`）、`TestRegistry` の text フラグ |
| フェーズ 3: 14 図種（boxgraph・chart、architecture の格子配置を含む） | 各図種の画像・罫線出力の golden と単体テスト |
| 各ステップの検証 | 最終状態で gofmt・vet・CGO_ENABLED=0 ビルド・全テスト（31 パッケージ）・4 通りのビルド・fuzz を実行 |
| golden の目視確認 | フェーズ 1・2 は作成時、フェーズ 3 の 15 枚は個別に確認 |
| フェーズごとの /adversarial-review | フェーズ 0（v0.6.4）、1（v0.6.5）、2（v0.6.6）、3（v0.6.3）、architecture の格子配置（v0.6.8）。見つかった不具合はすべて修正し回帰テストを追加 |

順序の注記: フェーズごとのレビューと golden の個別確認は、完了の宣言より後に行った。レビューで見つかった不具合は、すべてその時点で修正している。

## 最終レビュー（サブエージェントによる敵対的レビュー）後の修正（v0.6.9、2026-11-05）

深刻な指摘 4 件を直した。

1. **RenderText の flowchart が、既定の上限内の入力で 5〜13 秒かかった。** 原因は、5 つの試行のそれぞれで全エッジの経路を探索していたこと。次の 2 つで直した。
   - 配置の後、箱が並ぶ範囲の幅が `maxCols` を超える試行では、経路探索を省く。描いた図はこの幅より狭くならないので、結果は変わらない。開始・終了のグリフは中央の 1 桁として数える。
   - 経路探索の歩数に、1 回の呼び出しで 2²³ 歩の上限を設けた。既存のテストの最大は約 95 万歩。
   - 再現入力（60 ノード、499 エッジ）は、0.4 秒以内に `ErrTooLarge` を返す。回帰テストは `TestFinalReviewFixes`。
2. **`A--Yes-->B`（`--` の直後に空白が無い書き方）で、ラベルがノードになっていた。** 本家の lexer と同じく、閉じの `--` があればラベルとして読む。`==text==>` と `-.text.->` も同じ。既存の golden `flowchart_edge_cases` の `c -.text.-> start` も、誤って箱として記録されていたものが直った。
3. **sequence の `autonumber` が引数と `off` を無視していた。罫線出力には番号が出なかった。**
   - 開始番号と増分、`off` に対応した。
   - 番号の円は、番号の幅に合わせて大きくする。
   - 罫線出力では、番号をメッセージの前に書く。
4. **architecture-beta のエッジが、L 字で引かれていたため、サービスを突き抜けていた。**
   - サービスの辺と、そこから少し離した線でできる格子の上で、長さと曲がりに費用を付けた直交経路の探索をする（`internal/diagram/architecture/route.go`）。
   - 先に引いたエッジと重なる区間には費用を上乗せする。
   - 格子が 40000 点を超えるときは、L 字に戻す。

### 細かい指摘の修正（v0.6.10）

1. **class**: メンバーの先頭の `~`（package の可視性）が、総称型の印として `<` に置き換わっていた。`~` の数が奇数のときは、先頭を可視性として残す。
2. **front matter**: ブロック文字列（`|`、`>` とその `-`、`+` 付き）と、複数行にまたがるものを含むフロー形式（`{…}`、`[…]`）を読む。
3. **sequence の参加者**: Mermaid 11 の `participant A@{ "type": "database" }` を読む。`actor` は人型で描き、ほかの種類は「«種類»」の行で示す（NOTE(mermaid-compat)）。知らない種類は構文エラーにする。
4. **xychart の罫線出力**: 棒の長さを 0 の位置から測る。負の値は 0 の位置から左へ伸びる。
5. **flowchart の罫線出力**: `--o` と `--x` の端を、▶ ではなく ○ と × で描く。
6. **sequence のアクティベーション**: アクティブでない参加者の `deactivate`（`B->>-A` の形を含む）は、本家と同じく構文エラーにする。
7. **gantt**: 平日の残る長いタスクが「every day is excluded」になっていた。曜日の指定で 7 曜日すべてが除外されるときだけ、この文言にする。それ以外は「1 日あたり 7 日 + 除外日付の数」を歩数の上限とし、超えたら「duration is too long」にする。

回帰テストは `TestMinorReviewFixes`。golden は、○ と × を使う `flowchart_edge_cases.txt` と `flowchart_subgraph.txt` だけが変わった。

## 2 回目の最終レビュー（サブエージェント）後の修正（v0.6.11、2026-11-05）

1. **architecture-beta の経路探索に、全体の作業量の上限が無かった**（v0.6.9 で入った回帰。上限内の入力で最大 49 秒・12GB）。次のように直した。
   - 探索の歩数に、Render 1 回あたりの予算（800 万歩、約 1 秒）を設けた。MaxWidth に合わせた配置のやり直しの分も含む（`diagram.Env.Work`）。
   - 探索用の配列は使い回す。
   - 格子が大きすぎるときは、L 字に戻さずに `ErrTooLarge` にする。
   - 再レビューの再現入力（50 サービス・400 エッジ、98 サービス・304 エッジ）は、0.5〜1 秒で返る。
2. **`A---oB` と `A--text--xB`**: 本家の lexer と同じく、`--` の直後の `o` / `x` は常に端の印として読む。端の印も閉じも無い `A--B` は構文エラーにする。
3. **入れ子の総称型**: `List~List~int~~` を `List<List<int>>` と描く。`~` の開き・閉じをスタックで判定し、先頭の可視性の `~` とも両立させた。
4. **`autonumber`**: 本家の描画処理（`start || 現在の値`、`step || 現在の値`）に合わせ、引数を省くか 0 を渡すと、今の番号と増分を引き継ぐ。
5. **細かい指摘**:
   - front matter のフロー形式: 値の中の `'` を文字列の始まりとみなさない。読み切れない値は文字列として扱う。
   - architecture: エッジがグループのタイトルを避ける。
   - sequence の participant: `alias` を表示名にし、知らない種類も「«種類»」として示す（扱いを寛容な側にそろえた）。
   - Version の検査の穴（golden の名前の変更）: dal と同じく、`TestNoOrphanGoldens` と「無くなった golden」の検査を入れた。
