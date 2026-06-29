import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the GPSR heading", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { name: /gpsr compliance engine/i }),
    ).toBeInTheDocument();
  });
});
