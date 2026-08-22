import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { expect, test } from "vitest";
import { App, CrossExamNotes } from "./App";

test("renders the product name", () => {
  const html = renderToString(createElement(App));
  expect(html).toContain("Zeroth");
});

test("renders cross-exam notes inline", () => {
  const html = renderToString(
    createElement(CrossExamNotes, {
      exam: {
        verdict: "fail",
        reviewer_model: "sonnet",
        reasoning: "scope violation: .ssh/authorized_keys",
      },
    }),
  );
  expect(html).toContain("Cross-exam");
  expect(html).toContain("fail");
  expect(html).toContain("sonnet");
  expect(html).toContain("scope violation: .ssh/authorized_keys");
});
