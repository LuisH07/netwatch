<p align="center">
  <img src="https://raw.githubusercontent.com/devicons/devicon/master/icons/go/go-original.svg" width="90" alt="Go Logo" />
  &nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/devicons/devicon/master/icons/linux/linux-original.svg" width="90" alt="Linux Logo" />
  &nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/devicons/devicon/master/icons/git/git-original.svg" width="90" alt="Git Logo" />
  &nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/devicons/devicon/master/icons/github/github-original.svg" width="90" alt="GitHub Logo" />
</p>

<h1 align="center">NetWatch</h1>

<p align="center">
  Utilitário de linha de comando para Linux focado em diagnóstico rápido de conectividade e gerenciamento de Wi-Fi via D-Bus.
</p>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white" /></a>
  <a href="#"><img src="https://img.shields.io/badge/Cobra-CLI%20Framework-0052CC?style=for-the-badge&logo=code&logoColor=white" /></a>
  <a href="#"><img src="https://img.shields.io/badge/NetworkManager-D--Bus-FCC624?style=for-the-badge&logo=linux&logoColor=black" /></a>
  <a href="#"><img src="https://img.shields.io/badge/Platform-Linux-FCC624?style=for-the-badge&logo=linux&logoColor=black" /></a>
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" />
</p>

---

## Descrição

O **NetWatch** é uma ferramenta robusta e leve desenvolvida em **Go** para ambientes Linux, projetada para centralizar o diagnóstico de redes e o controle de conexões Wi-Fi sem a necessidade de realizar parsing de processos externos (como `nmcli` ou `iwconfig`).

A aplicação comunica-se diretamente com o **NetworkManager** através do barramento **D-Bus** do sistema operacional, garantindo alta performance, respostas síncronas precisas e baixo consumo de recursos. Além disso, executa verificações paralelas de latência ICMP, resolução DNS, portas TCP e integridade do gateway padrão.

### Compatibilidade de Distribuições
Projetado para rodar nativamente em distros Linux que utilizam o **NetworkManager** (testado e compatível com **Fedora, Arch Linux, Linux Mint e Gentoo**).

### Estado atual do projeto
* **Diagnóstico completo de rede (`check`)**: Validação de IP ativo, testes de latência ICMP, DNS e verificação de portas TCP em paralelo.
* **Gerenciamento de Wi-Fi (`wifi`)**: Listagem formatada de redes disponíveis com indicador de sinal (`%-3d%%`), suporte a detecção de segurança e conexão segura com leitura oculta de senha (`term.ReadPassword`).
* **Instalação inteligente**: Script automatizado (`install.sh`) com suporte a compilação local (se clonado com Go) ou download direto da release oficial do GitHub.
* **Arquitetura modular em Go**: Separação clara de comandos via `Cobra`, gerenciamento de erros customizados (`exitError`) e integração nativa com o D-Bus (`godbus/v5`).

---

## Tecnologias Utilizadas

### Linguagem e Core
* **Go (Golang)**
* **Cobra CLI** (Gerenciamento de comandos e subcomandos)

### Integração Linux e Sistemas
* **NetworkManager D-Bus API** (`github.com/godbus/dbus/v5`)
* **Golang x/term** (Leitura segura de senhas de terminais)

### Ferramentas e práticas
* **Git & GitHub**
* **Conventional Commits**
* **GitHub Actions (CI/CD & Releases)**

---

## Instalação e Execução

### Pré-requisitos
- **Linux** com **NetworkManager** ativo e D-Bus.
- Opcional (para desenvolvimento/compilação local): **Go** (versão 1.21+) e **Git**.

### Método de Instalação Inteligente (`install.sh`)

O script de instalação do NetWatch possui comportamento adaptativo automático: ele detecta se você está executando dentro de um repositório clonado com o Go instalado (compilando direto do código-fonte) ou se está rodando em um ambiente limpo (baixando o binário otimizado da última *Release* do GitHub).

#### Opção 1: Instalação Rápida Direta (Sem Clonar o Repositório)
Ideal para uso imediato em qualquer máquina Linux. Baixa o script e executa a instalação do binário oficial globalmente em uma única linha:

```bash
curl -sL https://raw.githubusercontent.com/LuisH07/netwatch/main/install.sh | bash

```

> **Nota:** Este método exige que o terminal esteja utilizando o interpretador `bash` para processar corretamente o fluxo recebido via pipe.

#### Opção 2: Instalação via Código-Fonte (Desenvolvimento)

Ideal se você clonou o projeto para testar modificações. O script detectará a presença do arquivo `go.mod` e o compilador Go local para gerar o build:

```bash
git clone [https://github.com/LuisH07/netwatch.git](https://github.com/LuisH07/netwatch.git)
cd netwatch
chmod +x install.sh
./install.sh
```

Após a conclusão por qualquer um dos métodos, o comando `netwatch` estará disponível globalmente em `/usr/local/bin` para uso em qualquer diretório do terminal.

## Autocompletar (Shell Completion)

Para habilitar o preenchimento automático de comandos com a tecla `Tab` no seu terminal, adicione a configuração correspondente ao seu shell:

### Para Bash

```bash
echo "source <(netwatch completion bash)" >> ~/.bashrc
source ~/.bashrc

```

### Para Zsh

```bash
echo "source <(netwatch completion zsh)" >> ~/.zshrc
source ~/.zshrc

```

## Mapeamento de Comandos

Abaixo estão listados todos os comandos suportados pelo NetWatch.

### Comandos Principais

| Comando | Descrição | Requer Conexão Wi-Fi? |
| --- | --- | --- |
| `netwatch --help` | Exibe a ajuda global da CLI | Não |
| `netwatch check` | Executa o diagnóstico completo de conectividade | Não |
| `netwatch wifi` | Exibe o status da interface e rede Wi-Fi atual | Não |
| `netwatch wifi list` | Lista todas as redes Wi-Fi (Access Points) disponíveis | Sim |
| `netwatch wifi connect [SSID]` | Conecta a uma rede Wi-Fi (solicita senha se protegida) | Não |
| `netwatch wifi disconnect` | Desconecta da rede Wi-Fi atual | Sim |

---

## Instruções de Uso

### 1. Diagnóstico de Conectividade (`check`)

O comando `check` valida a infraestrutura de rede local e testa a conectividade externa em paralelo:

```bash
netwatch check

```

* **O que avalia:** Interface IPv4 ativa, alcance do gateway padrão, latência ICMP, resolução de nomes (DNS) e handshake TCP.

### 2. Verificar Rede Wi-Fi Atual (`wifi`)

Para checar rapidamente se você está conectado a algum Access Point e qual interface está sendo utilizada:

```bash
netwatch wifi

```

### 3. Listar Redes Disponíveis (`wifi list`)

Para escanear e exibir uma tabela estruturada com os APs ao alcance (SSID, intensidade do sinal, tipo de segurança e BSSID):

```bash
netwatch wifi list

```

### 4. Conectar a uma Rede Wi-Fi (`wifi connect`)

```bash
netwatch wifi connect "NomeDaRede"

```

* **Comportamento inteligente:** O NetWatch detecta automaticamente se a rede é aberta ou protegida. Caso exija senha, ele solicita a digitação de forma segura pelo terminal (**ocultando os caracteres** para proteger contra exposição no histórico ou tela) e aguarda a confirmação de estado `Activated` do NetworkManager.

### 5. Desconectar (`wifi disconnect`)

Para encerrar a conexão Wi-Fi ativa na interface:

```bash
netwatch wifi disconnect

```

---

## Guia de Contribuição

O projeto segue padrões estritos de versionamento e colaboração.

1. Faça um **Fork** do projeto.
2. Crie uma branch para sua funcionalidade ou correção (`git checkout -b feature/nova-funcionalidade`).
3. Commit suas alterações seguindo o padrão **Conventional Commits** (`git commit -m 'feat(wifi): add auto-reconnect fallback'`).
4. Envie para a branch (`git push origin feature/nova-funcionalidade`).
5. Abra um **Pull Request**.

---

## Contribuidores

* **[Luís Henrique Domingos da Silva](https://github.com/LuisH07)**

---

## 📄 Licença

Este projeto está licenciado sob a **Licença MIT**.
Veja o arquivo [LICENSE](LICENSE) para mais detalhes.
