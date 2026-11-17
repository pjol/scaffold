import { llmsTxt } from "@/features/agent-web/llms";
import { agentWebManifest, agentWebOrigin, agentWebText } from "@/features/agent-web/server";

/** /llms.txt (llmstxt.org): the app for agents in one screen. */
export async function GET(req: Request) {
  return agentWebText(llmsTxt(agentWebOrigin(req), await agentWebManifest()));
}
