import { useMutation } from "@tanstack/react-query";
import { Download } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";

import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

/** Saves text as a file named name, through a temporary object URL. */
function saveFile(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * "Download my data": everything Perspectiv holds about the account, as one
 * JSON file. The server builds it and limits it to a few an hour.
 */
export function DataSection({ username }: { username: string }) {
  const exportData = useMutation({
    mutationFn: () => clients.user.exportMyData({}),
    onSuccess: (res) => {
      const day = new Date().toISOString().slice(0, 10);
      saveFile(`perspectiv-${username}-${day}.json`, res.json);
    },
  });
  return (
    <div className="grid gap-3">
      <p className="text-ink-muted">
        A file with everything Perspectiv keeps about your account: your profile, every story and draft, your claims,
        votes, reactions, thread posts, follows, blocks and reports.
      </p>
      <div>
        <Button variant="secondary" pending={exportData.isPending} onClick={() => exportData.mutate()}>
          <Download aria-hidden size={16} />
          Download my data
        </Button>
      </div>
      <p role="status" className="text-sm font-medium text-success empty:hidden">
        {exportData.isSuccess ? "Your data was downloaded." : ""}
      </p>
      <FormAlert message={exportData.error ? errorMessage(exportData.error) : null} />
    </div>
  );
}
