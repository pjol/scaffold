import { llmsFullTxt } from "@/features/agent-web/llms";
import { agentWebManifest, agentWebOrigin, agentWebText } from "@/features/agent-web/server";

/** /llms-full.txt: /llms.txt plus every listed page in full. */
export async function GET(req: Request) {
  const origin = agentWebOrigin(req);
  return agentWebText(await llmsFullTxt(origin, await agentWebManifest()), 600);
}
