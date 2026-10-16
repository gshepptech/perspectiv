import "@fontsource-variable/literata";
import "@fontsource-variable/public-sans";
import "./styles.css";

import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router";

import { TooltipProvider } from "@/components/ui/Tooltip";

import { createQueryClient } from "./queryClient";
import { createAppRouter } from "./router";

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root element");

const queryClient = createQueryClient();
const router = createAppRouter();

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
