import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";

import { http } from "@/lib/api";

import messages from "../../../../../../messages/ko.json";
import { ChatGPTOAuth } from "./chatgpt-oauth";

vi.mock("@/lib/api", () => ({ http: vi.fn() }));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.mocked(http).mockReset();
});

describe("ChatGPT OAuth panel (mock HTTP, real product component)", () => {
  it("opens the official authorization flow and keeps a link when popups are blocked", async () => {
    let pending = false;
    vi.mocked(http).mockImplementation(async (path) => {
      if (path.endsWith("/start")) {
        pending = true;
        return { url: "https://auth.openai.com/api/accounts/authorize?state=test" } as never;
      }
      return { connected: false, pending } as never;
    });
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    render(
      <NextIntlClientProvider locale="ko" messages={messages}>
        <ChatGPTOAuth onConfigured={vi.fn()} />
      </NextIntlClientProvider>,
    );
    await screen.findByText("ChatGPT 계정이 연결되지 않았습니다.");
    fireEvent.click(screen.getByRole("button", { name: "Continue with ChatGPT" }));
    const link = await screen.findByRole("link", { name: "로그인 창 열기" });
    expect(link.getAttribute("href")).toContain("https://auth.openai.com/");
    expect(open).toHaveBeenCalledOnce();
    expect(screen.getByRole("status").textContent).toContain("기다리는 중");
  });

  it("saves only a selected account model, without requesting an API key", async () => {
    vi.mocked(http).mockImplementation(async (path) => {
      if (path.endsWith("/models")) return { models: [{ id: "allowed-model", name: "Account model" }] } as never;
      if (path.endsWith("/configure")) return { id: 1 } as never;
      return { connected: true, pending: false, email: "test@example.invalid" } as never;
    });
    const configured = vi.fn().mockResolvedValue(undefined);
    render(
      <NextIntlClientProvider locale="ko" messages={messages}>
        <ChatGPTOAuth onConfigured={configured} />
      </NextIntlClientProvider>,
    );
    await screen.findByRole("option", { name: "Account model" });
    fireEvent.click(screen.getByRole("button", { name: "저장하고 이 모델 사용" }));
    await waitFor(() => expect(configured).toHaveBeenCalledOnce());
    expect(http).toHaveBeenCalledWith("/llm/oauth/chatgpt/configure", {
      method: "POST",
      body: JSON.stringify({ model: "allowed-model" }),
    });
  });

  it("shows a connection failure without reporting success", async () => {
    vi.mocked(http).mockRejectedValue(new Error("OAuth service unavailable"));
    render(
      <NextIntlClientProvider locale="ko" messages={messages}>
        <ChatGPTOAuth onConfigured={vi.fn()} />
      </NextIntlClientProvider>,
    );
    expect((await screen.findByRole("alert")).textContent).toContain("OAuth service unavailable");
  });
});
