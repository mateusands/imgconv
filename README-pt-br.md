# imgconv

*[Read in English](README.md)*

Um conversor de imagens escrito em Go. Converte entre **JPEG, PNG, GIF, TIFF e BMP** (e lê **WebP**),
com redimensionamento e controle de qualidade JPEG opcionais — um arquivo, ou uma pasta inteira em
paralelo. São três interfaces sobre um núcleo só: uma interface gráfica no navegador, uma linha de
comando para scripts, e uma interface de terminal interativa.

Ele é construído em torno de uma promessa: **nunca danifica o arquivo que você deu a ele.**

> Este é um projeto de aprendizado — o autor está usando para aprender Go. Ele foi escrito para ser
> lido, então o raciocínio por trás de cada decisão está no código e no [`AGENTS.md`](AGENTS.md).

## Instalação

**Baixe um arquivo e execute.** O imgconv é um único executável estático, sem nada para instalar ao
lado — sem runtime, sem bibliotecas, sem Go. Desinstalar é apagar o arquivo.

Pegue o da sua máquina em [Releases](https://github.com/mateusands/imgconv/releases):

| Sua máquina | Arquivo |
|---|---|
| Windows | `imgconv-<versão>-windows-amd64.exe` |
| macOS, Apple Silicon (M1 em diante) | `imgconv-<versão>-darwin-arm64` |
| macOS, Intel | `imgconv-<versão>-darwin-amd64` |
| Linux | `imgconv-<versão>-linux-amd64` |

O `SHA256SUMS` é publicado junto, se você quiser conferir o que baixou.

### Abrindo a interface gráfica

Executar com `--ui` sobe um pequeno servidor local, abre o navegador e imprime o endereço.
**Fechar a janela do terminal para o servidor** — não fica nada rodando em segundo plano.

**Windows** — duplo clique no `.exe`. Abre um console e o navegador em seguida. Para já abrir na
interface, crie um atalho e acrescente ` --ui` no destino.

**macOS** — `chmod +x imgconv-*-darwin-*` uma vez, depois duplo clique; o Finder executa no Terminal.

**Linux** — pelo terminal:

```bash
chmod +x imgconv-*-linux-amd64
./imgconv-*-linux-amd64 --ui
```

Duplo clique também funciona, mas alguns ambientes gráficos vão reportar o lançador como travado:
eles esperam uma janela aparecer, e um programa de linha de comando que abre um terminal nunca cria
uma janela própria. É cosmético — o programa roda, e fechar o terminal o encerra.

> ⚠️ **Estes binários não são assinados.** O Gatekeeper no macOS e o SmartScreen no Windows vão avisar
> sobre um desenvolvedor não identificado, porque assinar exige um certificado pago da Apple e da
> Microsoft. No macOS, botão direito → Abrir na primeira vez; no Windows, "Mais informações" →
> "Executar assim mesmo". Se essa troca não te agrada, compile você mesmo — são três linhas.

### Rodando a partir de um clone

Há um lançador para cada sistema, para não ter que digitar o comando toda vez:

| Sua máquina | Duplo clique |
|---|---|
| Linux | `run.sh` |
| macOS | `run.command` |
| Windows | `run.bat` |

**Cada um recompila antes de subir.** Isso é deliberado, não desperdício: a interface é compilada
para dentro do binário com `go:embed`, então mudar a página não muda nada até recompilar — e rodar um
binário velho é indistinguível de uma mudança que não funcionou. Um segundo compilando sai mais
barato que essa confusão. Fechar a janela encerra o servidor.

### Compilando você mesmo

Precisa de Go (veja a linha `go` no [`go.mod`](go.mod)).

```bash
git clone https://github.com/mateusands/imgconv.git
cd imgconv
go build -o imgconv ./cmd/imgconv
```

Para gerar os binários de todas as plataformas de uma vez, a partir de qualquer uma delas:

```bash
./scripts/build-release.sh v0.1.0     # escreve em dist/
```

Isso funciona sem nenhum compilador cruzado instalado porque o build usa `CGO_ENABLED=0` e o imgconv é
Go puro. É também o motivo de a interface gráfica ser servida ao navegador em vez de desenhada com um
toolkit nativo: toda biblioteca de GUI nativa em Go precisa de cgo, e isso custaria o binário único e
a compilação cruzada de um comando só.

## Uso

```
imgconv --ui                             a interface gráfica, no seu navegador
imgconv <entrada> -o <saída>             converte um arquivo; a extensão de -o escolhe o formato
imgconv <entrada> --to png               converte um arquivo, ao lado do original
imgconv <pasta> --to png --outdir <dir>  converte toda imagem de uma pasta, um nível
imgconv                                  sem argumentos: a interface de terminal
```

### A interface gráfica

`imgconv --ui` abre uma página no navegador com os arquivos que você escolheu, um seletor de formato,
controles de qualidade e redimensionamento, e um botão que abre o diálogo de arquivos do seu próprio
sistema. Ela converte do mesmo jeito que a linha de comando — mesmo código, mesmas garantias —
porque um comportamento diferente entre as duas seria um bug.

Ela escuta **somente em `127.0.0.1`**, e toda requisição precisa carregar um token gerado na hora
para aquela execução, então nada mais na sua máquina ou na sua rede consegue alcançá-la. O servidor
só pode ler os arquivos que você escolheu no diálogo — não existe navegação por pastas, e a página
nunca mostra um caminho do seu disco.

```bash
imgconv foto.jpg -o foto.png              # converte um arquivo
imgconv foto.jpg --to webp                # erro: aqui o webp é só leitura
imgconv foto.png -o menor.jpg --resize 800x --quality 85
imgconv ~/Imagens --to jpeg --outdir ~/saida --jobs 4
```

### Flags

| Flag | O que faz |
|---|---|
| `-o` | escreve neste caminho; a extensão escolhe o formato de destino |
| `--to` | formato de destino pelo nome, ex. `png` |
| `--outdir` | onde uma conversão de pasta escreve (obrigatório para ela) |
| `--force` | substitui um arquivo de saída existente |
| `--resize` | `LxA`, ou `Lx` / `xA` para manter a proporção |
| `--quality` | qualidade JPEG, 1–100; só para destino jpeg |
| `--jobs` | conversões simultâneas numa pasta (padrão: uma por CPU) |
| `--max-pixels` | recusa uma entrada cujo cabeçalho declare mais pixels que isso |

Um entre `-o` e `--to` é obrigatório — nenhum dos dois é adivinhado.

### Formatos

| Formato | Extensões | Lê | Escreve |
|---|---|---|---|
| JPEG | `.jpg` `.jpeg` | ✅ | ✅ |
| PNG | `.png` | ✅ | ✅ |
| GIF | `.gif` | ✅ | ✅ |
| TIFF | `.tif` `.tiff` | ✅ | ✅ |
| BMP | `.bmp` | ✅ | ✅ |
| WebP | `.webp` | ✅ | ❌ |

WebP é só leitura porque o `golang.org/x/image` traz um decodificador e nenhum codificador. Pedir ele
como destino dá um erro que diz isso, em vez de uma surpresa silenciosa.

**A extensão nunca é confiável na entrada.** Um `.png` que na verdade contém bytes de JPEG é lido como
JPEG, porque quem decide são os bytes mágicos. Extensões só servem para escolher o *destino*.

### Códigos de saída

A linha de comando é feita para scripts, então estes fazem parte da interface:

| Código | Significa |
|---|---|
| `0` | toda conversão pedida deu certo |
| `1` | uma conversão falhou — entrada ilegível, destino não suportado, erro de codificação |
| `2` | a invocação estava errada — flag ruim, argumento faltando |

Uma conversão de pasta que falhou em parte sai com código diferente de zero **e nomeia cada arquivo
que falhou** na saída de erro.

## O que ele se recusa a fazer

Estes são os motivos de o projeto existir, e cada um tem testes que foram vistos falhando antes de
passarem:

- **Não sobrescreve um arquivo existente** sem o `--force`.
- **Não escreve por cima da sua entrada, nem com `--force`.** A identidade entre saída e entrada é
  resolvida em descritores de arquivo abertos, então um link simbólico, um link físico ou
  `./a.jpg` versus `a.jpg` não conseguem enganá-lo.
- **Não deixa arquivo pela metade.** Uma codificação que falha no meio remove o que criou; com
  `--force`, escreve num temporário e só renomeia por cima depois que a codificação termina limpa,
  então uma falha deixa a sua saída anterior intacta.
- **Não converte uma pasta onde dois arquivos cairiam no mesmo nome de saída.** Ele recusa a execução
  inteira e lista as colisões antes, porque não existe um vencedor "pretendido" e o `--force` não
  escolhe um.
- **Não decodifica uma bomba de imagem, nem redimensiona para virar uma.** As duas alocações são
  limitadas antes de acontecer, porque o lado derivado de um redimensionamento vem da proporção da
  entrada — e a entrada é a coisa hostil aqui.
- **Não perde quadros em silêncio.** Achatar um GIF animado para um formato estático avisa, em todos
  os caminhos — a linha de comando, o terminal e a conversão de pasta.

## Desenvolvimento

```bash
go test ./...          # a suíte
go test -race ./...    # a conversão de pasta é concorrente; isto não é opcional
go vet ./...
gofmt -l . | tee /dev/stderr | (! read)     # gofmt -l sozinho sai 0 mesmo achando problema
```

Existe um alvo de fuzzing sobre o parser de quadros de GIF, que é a única parte escrita à mão que
interpreta entrada não confiável:

```bash
go test -run '^$' -fuzz=FuzzIsAnimated -fuzztime=60s ./internal/imageio/go_test/
```

### Estrutura

```
cmd/imgconv/       flags, códigos de saída, CLI-ou-TUI. Nenhuma lógica de imagem
internal/imageio/  o único pacote que importa image/* — decodifica, codifica, o registro de formatos
internal/convert/  transformações puras sobre um image.Image. Não abre arquivo
internal/pipeline/ o único lugar onde um arquivo é convertido: decodifica → transforma → codifica
internal/batch/    a execução paralela e limitada sobre uma pasta
internal/tui/      o modelo Bubble Tea
internal/web/      a interface gráfica servida ao navegador
```

A seta de dependências corre num sentido só — `cmd → {tui, web} → batch → pipeline → convert →
imageio` — e nunca volta. É isso que torna o núcleo de conversão testável sem terminal e sem pasta
temporária.

Todo formato suportado é uma linha de uma tabela em `internal/imageio`. O `--help`, a lista de
formatos da interface e o palpite pela extensão leem essa tabela, então adicionar um formato é
adicionar uma linha.

O [`AGENTS.md`](AGENTS.md) é o contrato completo, incluindo as armadilhas que já foram pagas.

## Dependências

A biblioteca padrão faz os formatos principais. Três módulos existem porque ela genuinamente não faz
TIFF, WebP nem interfaces de terminal:

| Módulo | Para quê | Licença |
|---|---|---|
| `golang.org/x/image` | TIFF, BMP, decodificação de WebP | BSD-3-Clause |
| `github.com/charmbracelet/bubbletea` | o laço de eventos do terminal | MIT |
| `github.com/charmbracelet/lipgloss` | o estilo do terminal | MIT |

## Licença

[MIT](LICENSE).
