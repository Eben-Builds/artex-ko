# ChatGPT 구독 OAuth 연결

시스템 → LLM의 **ChatGPT 구독 · OAuth**에서 **Continue with ChatGPT**를 누른다.
새 창에서 계정 로그인과 ChatGPT 구독 사용 권한에 직접 동의한다. ARTEX로 돌아오면
연결 계정의 모델 목록이 표시된다. 모델을 선택하고 **저장하고 이 모델 사용**을 누른다.
API 키를 입력하거나 기존 Codex 인증 파일을 복사할 필요가 없다.

이 기능은 OpenAI의 공개 Sign in with ChatGPT 오픈소스 앱 등록 흐름을 사용한다.
계정과 구독의 이용 자격·할당량이 적용되며, 무제한 무료 API를 제공하지 않는다.
로그인 동의만으로 추론 성공을 보장하지 않는다. 저장한 프로파일의 연결 테스트와
실제 `response.completed` 수신으로 모델 이용 가능 여부를 확인해야 한다.
로그인 창이 차단되면 패널의 **로그인 창 열기** 링크를 사용한다.

인증 시작은 로컬 주소 `http://localhost:8787` 또는 `http://127.0.0.1:8787`에서만
허용된다. 콜백은 `http://127.0.0.1:8787/auth/callback`이다. 원격 도메인이나
다른 장치에서 실행한 브라우저의 OAuth 콜백은 지원하지 않는다.

토큰은 `ARTEX_OAUTH_DIR/chatgpt.json`에 저장한다. 미지정 시 실행 파일 옆의
`.oauth/chatgpt.json`을 사용한다. 이 디렉터리를 파일 관리자가 제공하는 `data/`에
넣지 않는다. Unix에서는 디렉터리 0700·파일 0600으로 저장하고, 원자적으로 교체한다.
Docker에서는 별도 named volume으로 컨테이너 재생성 후에도 보존한다.
LLM 프로파일에는 계정 참조만 저장하며 브라우저·LLM 요청 기록에 토큰을 반환하지 않는다.

유효기간이 짧아지면 토큰을 자동 갱신한다. 갱신 실패·권한 철회·구독 한도 초과는
오류로 처리하며 다시 로그인이 필요할 수 있다. 새 로그인이 실패하면 기존 연결을
보존한다. 계정을 바꾸려면 먼저 연결을 해제한다. **이 앱에서 연결 해제**는 로컬
자격 증명을 제거한다. OpenAI 쪽 앱 권한 철회는 ChatGPT 설정에서 별도로 수행한다.

구독 프로파일 활성화 시 전역 LLM 자동 전환을 끄며, 이 프로파일을 시작점으로 한
일반 failover도 차단한다. 별도로 유료 프로파일을 포함한 작업별 체인을 사용자가
직접 지정하는 경우에는 해당 작업의 설정이 적용된다.

## Docker 로컬 빌드

기존 설치의 DB·`data/`를 그대로 사용한다. 실행 중인 작업을 먼저 확인한다.
Windows PowerShell에서는 다음 순서로 빌드한다.

```powershell
cd web
npm ci --include=dev
$env:NEXT_EXPORT='1'
npm run build
cd ..
New-Item -ItemType Directory -Force server/webui/dist | Out-Null
Copy-Item -Path web/out/* -Destination server/webui/dist -Recurse -Force
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go build -tags embedui -trimpath -ldflags '-s -w -X main.version=0.3.15-global.1-oauth.1' -o dist/amd64/artex ./cmd/artex
docker build -f Dockerfile.oauth -t artex-global:oauth-local .
New-Item -ItemType Directory -Force .secrets | Out-Null
docker compose cp artex:/app/jwt.key .secrets/jwt.key
Copy-Item docker-compose.oauth.yml docker-compose.override.yml
docker compose up -d artex
```

위 키 복사는 기존 설치를 업그레이드하는 경우에만 실행한다. 신규 설치는 원본
Compose로 먼저 기동해 키를 만든다. `.secrets/`, `.env`, OAuth 자격 증명을
Git에 추가하지 않는다. 원래 이미지로 돌아가려면 override 파일을 보관 위치로
옮긴 뒤 `docker compose up -d artex`를 실행한다. DB·volume은 삭제하지 않는다.

## 검증 경계

- Go OAuth 테스트는 가짜 인증 서버와 서명 키를 사용해 state·PKCE·서명·issuer·audience·nonce·scope,
  콜백 재사용 거부, 재시작 보존, 동시 갱신, 연결 해제를 검증한다.
- React 패널 테스트는 실제 컴포넌트와 모의 HTTP를 사용한다.
- 실제 계정 로그인·동의·모델 추론은 사용자 연결 후 별도로 확인해야 한다.
- Electron·Windows EXE 검증은 이 Docker 웹앱 변경의 대상이 아니다.

공식 명세:
[등록·로그인](https://developers.openai.com/siwc/token-sharing-open-source/sign-in),
[모델·추론](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference),
[제약](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).
