# ChatGPT OAuth 검증 기록 — 2026-11-06

실행 환경: Windows 호스트, Docker Desktop Linux 컨테이너, Ego Lite 0.6.2.8.

| 실행 그룹 | 상태 | 실제 검증 범위 |
| --- | --- | --- |
| OAuth·LLM 단위 회귀 | PASS | `go test ./chatoauth ./agent ./server -count=1`에서 실행된 테스트 통과. OAuth 전용 테스트는 모의 인증 서버·RSA 서명 키 사용. state·PKCE·OIDC·권한·재사용 거부·갱신·연결 해제·프로토콜·접근 제어 확인. |
| 웹 단위 | PASS | Vitest 6개 파일·16개 테스트. 새 OAuth 패널은 실제 제품 컴포넌트와 모의 HTTP를 사용. |
| 웹 빌드 | PASS | 다국어 키 검사, 변경 파일 Biome 검사(오류 0, 경고 2), TypeScript 포함 Next.js 정적 내보내기 31개 페이지. |
| 실제 웹 UI | PASS | Ego Lite에서 한국어 OAuth 패널, 실제 OpenAI 계정 선택·앱 등록·구독 사용 동의, 실제 콜백 성공 확인. 모의 IPC 없음. |
| 실제 모델 추론 | PASS | 앱의 `/api/llm/test`에서 `gpt-6.1-sol`, `ok=true`, `reply=OK`, 3377ms 확인. API 키 없이 OAuth 구독 토큰 사용. 스캔·침투 작업은 실행하지 않음. |
| 실제 재시작·DB | PASS | 실제 PostgreSQL에 구독 프로파일 저장·활성화, 유료 API 자동 전환 비활성화. 앱 컨테이너 재시작 후 연결과 활성 모델·기존 로그인 유지. |
| 시간 경과 후 실제 토큰 갱신 | NOT RUN | 실제 계정의 만료를 기다리거나 자격 증명을 변조하지 않음. 모의 서버의 자동 갱신·동시 갱신 직렬화·회전 토큰 저장은 단위 테스트에서 별도 PASS. |
| DB 전용 회귀 스위트 | NOT RUN | 위 Go 명령에서 별도 DB 설정이 필요한 테스트는 skip. 운영 DB를 테스트 DB로 사용하지 않음. skip은 PASS에 포함하지 않음. |
| Electron·Windows EXE·OS 대화상자 | NOT RUN | Docker 웹앱 변경이며 해당 실행 그룹은 대상이 아님. |

초기 실접속에서 호스트 ID 형식 오류를 발견했다. `urn:uuid:<UUIDv4>`로 수정하고
회귀 테스트·이미지 재빌드·실제 OAuth 인증·모델 추론을 다시 실행해 최종 통과했다.

## 실행 산출물 일치

- 버전: `0.3.15-global.1-oauth.1`
- 이미지: `artex-global:oauth-local`
- 최종 이미지 ID: `sha256:0e9f6edb5933e352ab778f33413b9c3296fb41e3d07111cf369eaf29f842c940`
- 빌드한 `dist/amd64/artex`와 컨테이너 `/app/artex`의 SHA-256 일치:
  `03b88db29bce5f74f50524248368273908358d19ded5e1e63c9fc6a066ed2bad`
- 공식 기본 이미지 digest와 재현 절차는 `Dockerfile.oauth` 및 `chatgpt-oauth.md` 참조.
- 실제 OAuth 토큰·JWT 서명 키·DB 비밀번호·사용자 이메일은 이 기록에 포함하지 않음.

설치 작업본과 운영 이미지·데이터·자격 증명 volume은 운영에 필요하므로 보존한다.
임시 Git worktree는 생성하지 않았다. 재생성 가능한 빌드 중간 산출물과 브라우저
자동화 스크립트는 작업 종료 시 정리한다.
