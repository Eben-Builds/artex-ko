# Windows 바탕화면 실행기

`scripts/Start-ARTEX.ps1`은 기존 Docker OAuth 설치를 로컬에서 실행한다.
바탕화면의 `ARTEX.lnk`에서 실행하면 준비가 끝난 뒤 기본 브라우저로
`http://localhost:8787`을 연다. 이미 실행 중이면 기존 인스턴스를 연다.

Docker 엔진이 꺼져 있으면 Docker Desktop을 숨김 실행하고 준비를 기다린다.
`desktop-linux` 컨텍스트를 명시하며 전역 Docker 컨텍스트는 바꾸지 않는다.
기존 Compose override를 요구하므로 OAuth 로컬 이미지를 공식 이미지로
의도치 않게 되돌리지 않는다. DB·설정·자격 증명은 기존 volume을 사용한다.

오류는 알림 창과 `%LOCALAPPDATA%\ARTEX\launcher.log`에서 확인한다.
콘솔 검증은 `powershell -NoProfile -File scripts/Start-ARTEX.ps1 -NoBrowser`로
브라우저를 열지 않고 수행할 수 있다.

아이콘은 기존 `web/public/logo.png`에서 Windows ICO로 변환하여
`%LOCALAPPDATA%\ARTEX\ARTEX.ico`에 설치한다. 바로가기는 사용자 Desktop
알려진 폴더에 만들며 PowerShell을 숨김 실행한다.

2026-11-06 Windows 검증: `-NoBrowser`로 실제 Compose 기동과 상태 API 확인 PASS.
바탕화면 바로가기의 대상 파일·아이콘 존재 확인 PASS. Docker Desktop이 완전히
종료된 상태에서의 자동 기동은 NOT RUN이며, 실제 운영 컨테이너를 검증 목적으로
종료하지 않았다. 임시 worktree나 테스트 프로필은 생성하지 않았다.
