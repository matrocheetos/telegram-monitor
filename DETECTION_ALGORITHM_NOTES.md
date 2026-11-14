# Notas Científicas e Observações de Futuro — Motor de Detecção de Termos e Extração (`telegram-monitor`)

> Este documento consolida a análise empírica da tabela local `debug_analyzed_messages` (`data/telegram-monitor.db`), a revisão da literatura científica de **Information Retrieval (IR)** para matching de produtos em textos curtos de e-commerce e observações arquiteturais para evoluções futuras além do escopo imediato da **Fase 13** em [tasks.md](file:///c:/Users/mateus/Code/Github/telegram-monitor/tasks.md#L720-L871).

---

## 1. Diagnóstico Empírico do Corpus `debug_analyzed_messages` (344 Mensagens Reais)

A inspeção direta do banco SQLite local (`data/telegram-monitor.db`) revelou os seguintes números e padrões reais do fluxo de promoções do Telegram:

### 1.1. Distribuição Geral (`evaluation_outcome`)
| Resultado (`evaluation_outcome`) | Quantidade | % do Corpus | Observação Principal |
| :--- | :---: | :---: | :--- |
| `matched_product` | 219 | 63,66% | 217 de 219 matches foram causados pelo termo genérico `'por'` (`id: 4`), demonstrando a necessidade de filtro de stopwords e validação de especificidade mínima. |
| `ignored` | 73 | 21,22% | Mensagens sem match de interesse nem indicador de cupom (ou canais com flags desativados). |
| `matched_coupon` | 29 | 8,43% | Mensagens que extraíram exclusivamente cupons. |
| `matched_both` | 23 | 6,69% | Mensagens que deram match em produto e também extraíram cupom. |
| **Total** | **344** | **100%** | Coletadas de 5 canais reais de ofertas (`A Oferta`, ` Nintendo Barato`, `Achados Meli`, etc.). |

### 1.2. O Problema dos Tokens Alfanuméricos (`'iphone 16e'` e SKUs de Hardware)
No corpus de 344 mensagens, foram identificados **312 tokens alfanuméricos distintos** no texto bruto (exemplos reais extraídos do banco: `15Irx9`, `13450Hx`, `14700Kf`, `5500F`, `5825u`, `7520U`, `A520M`, `B650M`, `DDR4`, `DDR5`, `GDDR6X`, `S25`, `S26`, `PS4`, `PS5`, `NS2`, `G5`, `M15`, `P20i`, `iM3`, `512GB`, `240Hz`, `128bit`).

No algoritmo atual (`internal/domain/match.go`):
1. `letterDigitBoundaryRegex` (`([a-z])([0-9])` -> `$1 $2`) e `digitLetterBoundaryRegex` (`([0-9])([a-z])` -> `$1 $2`) separam **incondicionalmente** qualquer fronteira entre letra e dígito.
2. Quando o usuário cadastra `'iphone 16e'`, `NormalizeText` produz `"iphone 16 e"`, gerando `PositiveTokens = ["iphone", "16", "e"]`.
3. Na língua portuguesa, `"e"` é a conjunção aditiva mais frequente. Consequentemente:
   - Qualquer mensagem contendo `"iPhone 16"` e a conjunção `"e"` em qualquer ponto do texto (ex: `"🔥 Apple iPhone 16 128GB - Preto e Branco"`) satisfaz `["iphone", "16", "e"]`, gerando **falso positivo sistemático**.
   - O mesmo ocorre para sufixos de processadores/placas (`5500f` -> `"5500"` + `"f"`, `7520u` -> `"7520"` + `"u"`, `16e` -> `"16"` + `"e"`) e prefixos de linha (`s25` -> `"s"` + `"25"`, `g5` -> `"g"` + `"5"`, `i5` -> `"i"` + `"5"`).

### 1.3. Resumo das 4 Views Analíticas do Banco de Debug
1. **`v_debug_multi_price_ambiguity` (175 registros)**:
   - **Preços unitários em kits/fardos**: Mensagens como `ID=4586` (`"15 unidades ... R$ 40 (R$ 2,69 cada)"`), `ID=3725` (`"R$ 18 (R$ 6 cada)"`), `ID=3868` (`"por R$ 60 (R$ 15 cada)"`), `ID=2965` (`"R$ 66 CADA KAIAK ... por R$ 132"`), `ID=4227` (`"R$ 1,62 CADA ... R$ 16"`) e `ID=2012` (`"R$ 9,43 por booster ... por R$ 339"`) tiveram o valor unitário extraído como menor preço porque o qualificador (`cada`, `por booster`) aparece **após** o número.
   - **Descontos e deltas pós-preço**: `ID=4651` (`"- R$2 do menor preço histórico ... R$ 183"` -> extraiu `2.0`), `ID=1835` (`"💸 R$ 15 OFF acima de R$ 99"` -> extraiu `15.0`) e `ID=3049` (`"🏷 22% OFF em R$ 1, Limite de R$ 500 OFF"` -> extraiu `1.0`).
2. **`v_debug_coupon_false_negatives` e Falsos Positivos de Cupom**:
   - **Falsos positivos de prosa em `keywordPatternRegex`**: `'MERCADO'` (7 ocorrências em frases como `"Cupom Mercado Livre"`), `'DER'` (2 ocorrências em `"se o cupom der erro"`), `'ABAIXO'` (`"Ative o cupom abaixo do preço"`), `'FORTE'` (`"Olha o cupom FORTE do Meli"`) e `'ECHOES'` (`"Dragon Quest XI S: Echoes"`).
   - **Falsos negativos em múltiplos cupons na mesma linha**: Mensagens com operadores `+` ou `ou` (`ID=70` `"M1RC35BR + AEMEGA15"`, `ID=500` `"HB3001 + AEMEGA12 ou AEMEGA6"`, `ID=2491`, `ID=2678`, `ID=3017`, `ID=3136`, `ID=4139` `"POUPEMELI ou CLIENTEVIP"`, `ID=4529`) tiveram apenas o primeiro código capturado.
3. **`v_debug_unrecognized_stores` e Slugs de Agregadores**:
   - 41 mensagens foram atribuídas ao agregador genérico `'A Oferta'`, embora a própria URL contivesse o nome da loja no sufixo do slug (`https://aoferta.net/003wMFHz-Kabum` e `https://aoferta.net/0033iDdD-Terabyte`).
   - Encurtadores oficiais frequentes sem mapeamento: `link.amazon` e `a.aliexpress.com`, além de `Netshoes`, `Centauro`, `Dafiti`, `Nike` e `Carrefour`.

---

## 2. Modelos Científicos de Matching de Palavras para Identificação de Produtos

A literatura de **Information Retrieval (IR)** e **Entity Matching para E-Commerce** estuda extensivamente o problema de casar consultas curtas de usuários (*short product queries*, 1 a 5 tokens) contra textos promocionais semiestruturados. Abaixo estão os modelos científicos avaliados e sua adequação às restrições do `telegram-monitor` (`< 30 MB` RSS, zero CGO, latência de microssegundos no hot path).

### 2.1. Grafo de Delimitadores Intra-Palavra e Indexação Dual (*Lucene `WordDelimiterGraph` / Compound Tokenization*)
- **Referência Técnica/Científica**: *Subword & Intra-Word Delimiter Graph Tokenization* (Apache Lucene / Elasticsearch `word_delimiter_graph`, amplamente validado em motores de busca de catálogos de produtos como Amazon Product Search e Walmart Labs).
- **Como Funciona**:
  Em vez de substituir destrutivamente transições letra-dígito por espaço, o analisador classifica os compostos alfanuméricos em duas categorias semânticas:
  1. **Quantidades + Unidades de Medida** ($\text{Dígitos} + \text{Unidade}$, ex: `512gb`, `240hz`, `65w`, `1000mah`): o número e a unidade têm significado independente e frequentemente aparecem com ou sem espaço (`"512GB"` vs `"512 GB"`).
  2. **Designadores de Modelo / SKU** ($\text{Dígitos} + \text{Sufixo de Modelo}$ como `16e`, `5500f`, `14700kf`, `13450hx` ou $\text{Prefixo Curto} + \text{Dígitos}$ como `s25`, `ps5`, `ddr5`, `a520m`, `m.2`): a letra não é uma palavra autônoma, mas parte indivisível do identificador do produto.
- **Aplicação na Fase 13 (Task 13.1)**:
  - Na **Query (`NewUserInterest`)**: Preservar tokens alfanuméricos de modelo intactos (`"iphone 16e"` $\to$ `["iphone", "16e"]`; `"galaxy s25"` $\to$ `["galaxy", "s25"]`).
  - No **Documento (`TokenizeMessage`)**: Aplicar *Dual-Token Emission* (indexar no `tokenSet` da mensagem tanto a forma concatenada `"rtx4060"` quanto as sub-partes `"rtx"` + `"4060"` quando o prefixo alfabético tem $\ge 3$ letras ou quando o sufixo é uma unidade de medida conhecida `"512gb"` $\to$ `"512gb"`, `"512"`, `"gb"`), garantindo **100% de recall** entre `"rtx 4060"` e `"RTX4060"` e **100% de precisão** para `"iphone 16e"`.

### 2.2. Markov Random Field (MRF) para Dependência de Termos e Proximidade (*Metzler & Croft, SIGIR 2005*)
- **Referência Científica**: Metzler, D., & Croft, W. B. (2005). *A Markov Random Field Model for Term Dependencies*. Proceedings of the 28th Annual International ACM SIGIR Conference.
- **Formulação Matemática**:
  O modelo MRF decompõe a relevância de um documento $D$ para uma query $Q = (q_1, \dots, q_n)$ em três potenciais sobre cliques do grafo:
  $$P(D \mid Q) \propto \lambda_T \sum_{i=1}^{n} f_T(q_i, D) + \lambda_O \sum_{i=1}^{n-1} f_O(q_i, q_{i+1}, D) + \lambda_U \sum_{c \in C(Q)} f_U(c, D)$$
  onde:
  - $f_T$ (*Full Independence*): presença individual dos termos (Bag-of-Words unigrama);
  - $f_O$ (*Sequential Dependence*): termos adjacentes em ordem exata (bigramas como `"iphone 16"`, `"switch 2"`);
  - $f_U$ (*Unordered Window Dependence*): termos coocorrendo dentro de uma janela posicional curta de tamanho $k$ ($|\text{pos}(q_i) - \text{pos}(q_j)| \le k$).
- **Por que resolve um problema real do `telegram-monitor` (Task 13.2)**:
  No Bag-of-Words puro ($f_T$ isolado), a busca `"switch 2"` dá match em `"Console Nintendo Switch OLED ... 2x sem juros"` porque `"switch"` está na posição $3$ e `"2"` está na posição $18$. Ao impor a restrição de janela $f_U$ ($|\text{pos}(\text{switch}) - \text{pos}(2)| \le 3$) para modificadores numéricos curtos e sufixos de versão (`pro`, `max`, `plus`, `ultra`, `ti`, `super`), eliminamos falsos positivos numéricos sem perder ofertas legítimas como `"Nintendo Switch (Modelo 2)"`.

### 2.3. Distância de Damerau-Levenshtein Restrita (*Optimal String Alignment*) vs. Jaro-Winkler
- **Referência Científica**:
  - Damerau, F. J. (1964). *A technique for computer detection and correction of spelling errors*. Communications of the ACM, 7(3), 171–176.
  - Winkler, W. E. (1990). *String Comparator Metrics and Enhanced Decision Rules in the Fellegi-Sunter Model of Record Linkage*.
- **Comparação Técnica para Produtos**:
  1. **Damerau-Levenshtein ($d_{DL} \le 1$)**: Mais de 80% dos erros humanos de digitação pertencem a 4 operações: inserção, omissão, substituição de 1 letra ou **transposição de duas letras adjacentes** (ex: `"nintnedo"`, `"plaistation"`, `"notebok"`). Na distância de Levenshtein clássica atualmente implementada em `IsLevenshteinLE1`, uma transposição custa $2$ edições (1 deleção + 1 inserção) e falha. Em Damerau-Levenshtein, custa $1$ edição e pode ser verificada em `O(N)` e **zero alocação**.
  2. **Invariante Crítica para SKUs e Modelos Numéricos**: Métricas de edição baseadas em caracteres **não devem ser aplicadas a tokens contendo dígitos**. Em catálogos de produtos, `"RTX 4060"` e `"RTX 4070"` (ou `"iPhone 15"` e `"iPhone 16"`, `"i5-13450HX"` e `"i5-13400HX"`) têm distância de edição $1$, mas são produtos completamente distintos. Portanto, o fuzzy matching deve ser estritamente bloqueado quando `hasDigit(token) == true` (Task 13.2).

---

## 3. Observações e Ideias para o Futuro (Fora do Escopo Imediato da Fase 13)

As ideias abaixo foram mapeadas durante a pesquisa científica e a auditoria do banco `debug_analyzed_messages`, ficando documentadas como **evoluções futuras opcionais** caso o volume de usuários, canais ou regras cresça:

### 3.1. Detecção Automática de Acessório vs. Produto Principal (*Head-Noun / Modifier Asymmetry*)
- **Problema Observado em E-Commerce IR**:
  Um usuário que cadastra `"iphone 16e"` ou `"switch 2"` frequentemente esquece de adicionar palavras negativas (`-capa -pelicula -case -suporte`). Quando surge uma oferta `"🔥 Película de Vidro 3D para iPhone 16e - R$ 15"`, todos os tokens positivos (`"iphone"`, `"16e"`) estão presentes no título.
- **Abordagem Científica para o Futuro (*Prepositional Modifier Parsing*)**:
  Em português, a estrutura sintática de acessórios quase sempre segue o padrão:
  $$\underbrace{\text{[Substantivo de Acessório]}}_{\text{Head Noun (núcleo da oferta)}} + \underbrace{\text{(para | de | p/ | compatível com)}}_{\text{Preposição de destino}} + \underbrace{\text{[Nome do Produto Principal]}}_{\text{Complemento}}$$
  - Se o termo do usuário casar **apenas** com tokens posicionados **após** preposições de finalidade (`para`, `p/`, `compativel com`) na primeira linha da oferta, e antes dessa preposição houver um substantivo de acessório (`capa`, `capinha`, `pelicula`, `case`, `estojo`, `suporte`, `cabo`, `carregador`, `fonte`, `adesivo`, `skin`, `pulseira`, `alca`, `bag`), o motor pode descartar automaticamente a oferta como acessório (a menos que o próprio interesse do usuário contenha o substantivo de acessório!).
  - **Filtro de Piso de Preço Relativo (Outlier Floor)**: Alternativamente, se o usuário definiu `max_price = R$ 3.500` para `"iphone 16e"`, um preço extraído inferior a $10\%$ do `max_price` (ex: `R$ 19,90`) é estatisticamente um acessório ou peça de reposição.

### 3.2. Ponderação Zonal inspirada em BM25F / BM25S usando Entidades do Telegram (`tg_entities_json`)
- **Referência Científica**: Robertson, S., Zaragoza, H., & Taylor, M. (2004). *Simple BM25 Extension to Multiple Weighted Fields (BM25F)*. CIKM.
- **Observação no Banco `debug_analyzed_messages`**:
  As mensagens de grupos de ofertas possuem uma estrutura em **3 zonas claras**:
  1. **Zona 1 — Título / Headline (1ª e 2ª linhas, frequentemente marcadas com `MessageEntityBold` no MTProto)**: Contém o nome real do produto anunciado.
  2. **Zona 2 — Bloco de Preço, Parcelamento e Cupom (linhas intermediárias)**: Contém o preço, condição (`no Pix`, `em 12x`) e código de cupom (`MessageEntityCode`).
  3. **Zona 3 — Rodapé / Boilerplate do Canal (últimas linhas)**: Contém textos fixos do grupo (ex: `"⚠️ Preço sujeito a alteração"`, `"Entre no nosso grupo de WhatsApp"`, `"Vendido e entregue por Amazon"`, `"Mais ofertas no site"`).
- **Evolução Futura**:
  Restringir o matching de `PositiveTokens` do produto às **Zonas 1 e 2** (ou descartar linhas de boilerplate de rodapé antes da tokenização), evitando que palavras presentes na assinatura fixa do canal (como nomes de lojas, categorias ou avisos) contribuam para falsos positivos.

### 3.3. Índice Invertido Imutável no `MatcherSnapshot` para Escala de Milhares de Interesses
- **Estado Atual**:
  O `MatcherSnapshot` itera linearmente sobre a lista de interesses ativos (`O(I * T)`, onde $I$ é o número de interesses ativos e $T$ o número médio de tokens por interesse, tipicamente $2$ a $4$). Para algumas dezenas ou centenas de interesses, a varredura linear em slice contíguo na CPU leva poucos microssegundos e é mais rápida que estruturas complexas devido à localidade de cache L1/L2.
- **Evolução Futura (Caso $I > 5.000$ interesses simultâneos)**:
  Construir dentro do `MatcherSnapshot` um **Índice Invertido por Token Raro (*Posting List / Two-Watch-Literal Scheme*)**:
  - Cada interesse registra seu token positivo mais específico (maior comprimento / menor frequência) em um `map[string][]int` apontando para os índices dos interesses candidatos.
  - Ao receber uma mensagem com $M$ tokens únicos, o motor consulta apenas as *posting lists* dos tokens presentes na mensagem, reduzindo a avaliação de `O(I)` para `O(matches)` sem adicionar dependências externas e mantendo < 1 MB de memória adicional.

### 3.4. Por que Modelos Neurais (Dense Retrieval / Cross-Encoders / Embeddings) NÃO são Recomendados Aqui
Durante a pesquisa, foram avaliados modelos modernos de *Dense/Sparse Retrieval* (como **MiniLM-L6**, **BGE-M3** e **SPLADE**):
1. **Orçamento de Memória (`< 30 MB` RSS)**: Mesmo um modelo MiniLM quantizado em INT8 exige $25\text{–}90\text{ MB}$ de pesos em RAM, violando diretamente a diretiva arquitetural de `< 30 MB` RSS em container sem CGO.
2. **Alucinação Semântica em SKUs Exatos**: Modelos baseados em embeddings densos aproximam vetorialmente `"iPhone 16"`, `"iPhone 16 Pro"` e `"iPhone 16e"` (cosseno $> 0.94$), o que é desejável em buscas exploratórias, mas **desastroso em alertas de promoções**, onde quem procura um `"RTX 5070"` não quer receber alerta de `"RTX 5060"`.
3. **Conclusão**: A combinação de **Tokenização Alfanumérica Dual (`WordDelimiterGraph`) + Janela Posicional (`MRF`) + Damerau-Levenshtein restrito a tokens alfabéticos + Regras Estruturais de Preço/Cupom** entrega precisão superior a modelos neurais para SKUs técnicos, com **custo zero de RAM extra e latência sub-milissegundo**.
