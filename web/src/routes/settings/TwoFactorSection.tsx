import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Download, ShieldCheck } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";
import { updateSession } from "@/lib/session";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";
import { anyCodeSchema, appCodeSchema, passwordSchema } from "@/routes/auth/rules";

import { sessionsKey } from "./DeviceList";
import { Saved } from "./Section";

// Query keys follow ["domain", "operation", ...inputs].
const twoFactorStatusKey = ["auth", "getTwoFactorStatus"] as const;

const day = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

const WRONG_PASSWORD = "Your current password is not right.";
const WRONG_CODE = "That code is not right.";
// Wrong codes and passwords come back as these codes; each is said under its
// own field, so neither is repeated as a form-level alert.
const fieldCodes = [Code.PermissionDenied, Code.InvalidArgument];

function codeOf(err: unknown): Code {
  return ConnectError.from(err).code;
}

function formFailure(err: Error | null): string | null {
  if (!err || fieldCodes.includes(codeOf(err))) return null;
  return errorMessage(err, {
    [Code.ResourceExhausted]: "Too many wrong tries. Wait a while and try again.",
  });
}

/** Saves text as a file named name, through a temporary object URL. */
function saveFile(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

/** A key in groups of four, the way it is easiest to type into an app by hand. */
function grouped(key: string): string {
  return key.replace(/(.{4})(?=.)/g, "$1 ");
}

/** A copy-to-clipboard button whose result is said in a status line. */
function CopyButton({ text, label, done }: { text: string; label: string; done: string }) {
  const [said, setSaid] = useState<string | null>(null);
  return (
    <>
      <Button
        variant="ghost"
        onClick={() => {
          navigator.clipboard
            .writeText(text)
            .then(() => setSaid(done))
            .catch(() => setSaid("Copying did not work here. Select the text and copy it instead."));
        }}
      >
        <Copy aria-hidden size={16} />
        {label}
      </Button>
      <span role="status" className="text-sm text-ink-muted empty:hidden">
        {said ?? ""}
      </span>
    </>
  );
}

/**
 * Step 3: the recovery codes, shown once. They are only ever replaced, never
 * shown again, so the step says so and offers a file and a copy.
 */
function RecoveryCodes({ codes, username, onDone }: { codes: string[]; username: string; onDone: () => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const file = () => {
    const date = new Date().toISOString().slice(0, 10);
    return [`Perspectiv recovery codes for ${username}`, `Made ${date}. Each code signs you in once.`, "", ...codes, ""].join("\n");
  };
  return (
    <div className="grid gap-3">
      <h4 ref={heading} tabIndex={-1} className="font-medium focus:outline-none">
        Save your recovery codes
      </h4>
      <p>Each code signs you in once if you lose your phone. This is the only time we show them.</p>
      <ul aria-label="Recovery codes" className="grid gap-x-8 gap-y-1 font-mono text-base min-[400px]:grid-cols-2">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" onClick={() => saveFile("perspectiv-recovery-codes.txt", file())}>
          <Download aria-hidden size={16} />
          Download as a text file
        </Button>
        <CopyButton text={codes.join("\n")} label="Copy codes" done="Codes copied." />
      </div>
      <div>
        <Button onClick={onDone}>I&apos;ve saved my codes</Button>
      </div>
    </div>
  );
}

const passwordStep = z.object({ currentPassword: passwordSchema });
type PasswordValues = z.infer<typeof passwordStep>;

/** Step 1: the current password, so a borrowed session cannot turn it on. */
function PasswordStep({ onSetup, onCancel }: { onSetup: (uri: string, secret: string) => void; onCancel: () => void }) {
  const form = useForm<PasswordValues>({ resolver: zodResolver(passwordStep), defaultValues: { currentPassword: "" } });
  const begin = useMutation({
    mutationFn: (v: PasswordValues) => clients.auth.beginTwoFactorSetup(v),
    onSuccess: (res) => onSetup(res.otpauthUri, res.secret),
    onError: (err) => {
      if (codeOf(err) === Code.PermissionDenied) form.setError("currentPassword", { message: WRONG_PASSWORD }, { shouldFocus: true });
    },
  });
  return (
    <Form {...form}>
      <form noValidate className="grid gap-4" onSubmit={form.handleSubmit((v) => begin.mutate(v))}>
        <FormField
          control={form.control}
          name="currentPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Current password</FormLabel>
              <FormControl>
                <Input type="password" autoComplete="current-password" autoFocus {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormAlert message={formFailure(begin.error)} />
        <div className="flex flex-wrap gap-3">
          <Button type="submit" pending={begin.isPending}>
            Continue
          </Button>
          <Button variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </form>
    </Form>
  );
}

const appCodeStep = z.object({ code: appCodeSchema });
type CodeValues = { code: string };

/** Step 2: add the secret to an app, by QR code or by key, and prove it with a code. */
function ScanStep({ uri, secret, onCodes, onCancel }: {
  uri: string;
  secret: string;
  onCodes: (codes: string[]) => void;
  onCancel: () => void;
}) {
  const heading = useRef<HTMLParagraphElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useForm<CodeValues>({ resolver: zodResolver(appCodeStep), defaultValues: { code: "" } });
  const confirm = useMutation({
    mutationFn: (v: CodeValues) => clients.auth.confirmTwoFactorSetup(v),
    onSuccess: (res) => onCodes(res.recoveryCodes),
    onError: (err) => {
      if (codeOf(err) === Code.InvalidArgument) {
        form.setError("code", { message: `${WRONG_CODE} Check that your phone's clock is set automatically.` }, { shouldFocus: true });
      }
    },
  });
  return (
    <div className="grid gap-4">
      <p ref={heading} tabIndex={-1} className="font-medium focus:outline-none">
        Add Perspectiv to your authenticator app
      </p>
      <ol className="grid list-decimal gap-6 ps-5">
        <li className="grid gap-3">
          <p>Scan this code with your authenticator app.</p>
          {/* Scanners expect dark modules on a light ground, so the square keeps
              the light theme's paper and ink in the dark theme too. */}
          <div className="justify-self-start rounded-md border border-rule bg-paper p-4 text-ink scheme-light">
            <QRCodeSVG
              value={uri}
              size={192}
              level="M"
              role="img"
              aria-label="QR code for adding Perspectiv to your authenticator app"
              bgColor="transparent"
              fgColor="currentColor"
            />
          </div>
          <p className="text-sm">Can&apos;t scan it? Enter this key instead:</p>
          <p className="font-mono text-base [overflow-wrap:anywhere] select-all">{grouped(secret)}</p>
          <div className="flex flex-wrap items-center gap-3">
            <CopyButton text={secret} label="Copy key" done="Key copied." />
          </div>
        </li>
        <li>
          <Form {...form}>
            <form noValidate className="grid gap-4" onSubmit={form.handleSubmit((v) => confirm.mutate(v))}>
              <p>Enter the six-digit code the app shows.</p>
              <FormField
                control={form.control}
                name="code"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Code from your app</FormLabel>
                    <FormControl>
                      <Input inputMode="numeric" autoComplete="one-time-code" maxLength={7} className="max-w-40" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormAlert
                message={
                  confirm.error && codeOf(confirm.error) === Code.FailedPrecondition
                    ? "This setup has expired. Cancel and start again."
                    : formFailure(confirm.error)
                }
              />
              <div className="flex flex-wrap gap-3">
                <Button type="submit" pending={confirm.isPending}>
                  Turn on
                </Button>
                <Button variant="secondary" onClick={onCancel}>
                  Cancel
                </Button>
              </div>
            </form>
          </Form>
        </li>
      </ol>
    </div>
  );
}

const passwordAndCode = z.object({ currentPassword: passwordSchema, code: anyCodeSchema });
type PasswordAndCodeValues = z.infer<typeof passwordAndCode>;

/** A dialog that needs the current password and a current code before it acts. */
function PasswordAndCodeDialog<T>({ trigger, title, description, confirm, act, onDone }: {
  trigger: string;
  title: string;
  description: string;
  confirm: string;
  act: (v: PasswordAndCodeValues) => Promise<T>;
  onDone: (result: T) => void;
}) {
  const [open, setOpen] = useState(false);
  const form = useForm<PasswordAndCodeValues>({
    resolver: zodResolver(passwordAndCode),
    defaultValues: { currentPassword: "", code: "" },
  });
  const run = useMutation({
    mutationFn: act,
    onSuccess: (result) => {
      setOpen(false);
      form.reset();
      onDone(result);
    },
    onError: (err) => {
      if (codeOf(err) === Code.PermissionDenied) form.setError("currentPassword", { message: WRONG_PASSWORD }, { shouldFocus: true });
      if (codeOf(err) === Code.InvalidArgument) form.setError("code", { message: WRONG_CODE }, { shouldFocus: true });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          form.reset();
          run.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary">{trigger}</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
        <Form {...form}>
          <form noValidate className="mt-4 grid gap-4" onSubmit={form.handleSubmit((v) => run.mutate(v))}>
            <FormField
              control={form.control}
              name="currentPassword"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Current password</FormLabel>
                  <FormControl>
                    <Input type="password" autoComplete="current-password" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="code"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Code from your app or a recovery code</FormLabel>
                  <FormControl>
                    <Input autoComplete="one-time-code" autoCapitalize="none" spellCheck={false} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormAlert message={formFailure(run.error)} />
            <div className="mt-2 flex flex-wrap justify-end gap-3">
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
              <Button type="submit" pending={run.isPending}>
                {confirm}
              </Button>
            </div>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

/** How many recovery codes are left, warning when they run low. */
function CodesLeft({ left }: { left: number }) {
  if (left === 0) return <p className="text-warning">No recovery codes left. Make new ones.</p>;
  const text = left === 1 ? "1 recovery code left." : `${left} recovery codes left.`;
  return <p className={cn(left <= 3 && "text-warning")}>{left <= 3 ? `${text} Make new ones soon.` : text}</p>;
}

type Step = { kind: "status" } | { kind: "password" } | { kind: "scan"; uri: string; secret: string } | { kind: "codes"; codes: string[] };

/**
 * Two-factor sign-in in Settings: turning it on (password, then the app,
 * then the recovery codes), and once on, making new recovery codes or turning
 * it off, both behind the password and a current code.
 */
export function TwoFactorSection({ username }: { username: string }) {
  const queryClient = useQueryClient();
  const status = useQuery({ queryKey: twoFactorStatusKey, queryFn: () => clients.auth.getTwoFactorStatus({}) });
  const [step, setStep] = useState<Step>({ kind: "status" });
  const [said, setSaid] = useState<string | null>(null);
  const anchor = useRef<HTMLDivElement>(null);
  // The admin notice links to #two-factor; bring the block into view once it
  // has something to show.
  useEffect(() => {
    if (status.isSuccess && window.location.hash === "#two-factor") anchor.current?.scrollIntoView({ block: "start" });
  }, [status.isSuccess]);

  const reread = () => queryClient.invalidateQueries({ queryKey: twoFactorStatusKey });
  const toStatus = () => setStep({ kind: "status" });

  let body: React.ReactNode;
  if (step.kind === "password") {
    body = <PasswordStep onSetup={(uri, secret) => setStep({ kind: "scan", uri, secret })} onCancel={toStatus} />;
  } else if (step.kind === "scan") {
    body = (
      <ScanStep
        uri={step.uri}
        secret={step.secret}
        onCancel={toStatus}
        onCodes={(codes) => {
          updateSession({ twoFactorEnabled: true });
          void reread();
          // Turning it on signs out every other device; the list must say so.
          void queryClient.invalidateQueries({ queryKey: sessionsKey });
          setSaid("Two-factor sign-in is on.");
          setStep({ kind: "codes", codes });
        }}
      />
    );
  } else if (step.kind === "codes") {
    body = <RecoveryCodes codes={step.codes} username={username} onDone={toStatus} />;
  } else if (status.isPending) {
    body = <p className="text-ink-muted">Loading…</p>;
  } else if (status.isError) {
    body = <FormAlert message={errorMessage(status.error)} />;
  } else if (status.data.enabled) {
    body = (
      <div className="grid gap-3">
        <p className="flex items-center gap-1.5">
          <ShieldCheck aria-hidden size={16} className="shrink-0 text-success" />
          {status.data.enabledAt ? `On since ${day.format(timestampDate(status.data.enabledAt))}.` : "On."}
        </p>
        <CodesLeft left={status.data.recoveryCodesLeft} />
        <Saved message={said} />
        <div className="flex flex-wrap gap-3">
          <PasswordAndCodeDialog
            trigger="Make new recovery codes…"
            title="Make new recovery codes?"
            description="Your old codes stop working. You will see the new ones once."
            confirm="Make new codes"
            act={(v) => clients.auth.regenerateRecoveryCodes(v)}
            onDone={(res) => {
              void reread();
              setSaid(null);
              setStep({ kind: "codes", codes: res.recoveryCodes });
            }}
          />
          <PasswordAndCodeDialog
            trigger="Turn off…"
            title="Turn off two-factor sign-in?"
            description="Signing in will ask only for your password. Your recovery codes stop working."
            confirm="Turn off"
            act={(v) => clients.auth.disableTwoFactor(v)}
            onDone={() => {
              updateSession({ twoFactorEnabled: false });
              void reread();
              setSaid("Two-factor sign-in is off.");
            }}
          />
        </div>
      </div>
    );
  } else {
    body = (
      <div className="grid gap-3">
        <p>Off. With it on, signing in asks for a code from an authenticator app on your phone as well as your password.</p>
        <Saved message={said} />
        <div>
          <Button
            variant="secondary"
            onClick={() => {
              setSaid(null);
              setStep({ kind: "password" });
            }}
          >
            Turn on two-factor sign-in…
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div id="two-factor" ref={anchor} className="grid scroll-mt-4 gap-3" role="group" aria-labelledby="security-two-factor">
      <h3 id="security-two-factor" className="text-lg">
        Two-factor sign-in
      </h3>
      {body}
      <p className="text-sm text-ink-muted">
        Lost your phone and every recovery code? Write to us from this account&apos;s email address. We check it is you before we
        turn two-factor sign-in off, and we email you when we do.
      </p>
    </div>
  );
}
