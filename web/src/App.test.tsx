import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { expect, test } from "vitest";
import { App } from "./App";

test("renders the product name", () => {
  const html = renderToString(createElement(App));
  expect(html).toContain("Zeroth");
});
