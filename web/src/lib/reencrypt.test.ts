import { describe, expect, it } from "vitest";

import { ApiRequestError } from "./api";
import {
  KDF_PARAMS,
  bytesToB64,
  decryptNoteContent,
  deriveKey,
  encryptNoteContent,
} from "./crypto";
import {
  countRemaining,
  needsPreviousPassword,
  planMigration,
  runMigration,
  verifyOldPassword,
  type EncryptedItem,
  type MigrationReport,
  type MigrationRow,
  type ReencryptDeps,
} from "./reencrypt";

// Node 24 自带 WebCrypto，这里用真实派生与真实加解密，只把 PUT 换成内存假实现。
const SALT = bytesToB64(new Uint8Array(16).fill(7));

function keyFor(password: string): Promise<CryptoKey> {
  return deriveKey(password, SALT, KDF_PARAMS);
}

async function sealed(
  key: CryptoKey,
  id: string,
  plain: string,
): Promise<EncryptedItem> {
  return { id, cipher_b64: await encryptNoteContent(key, id, plain) };
}

interface Recorder {
  deps: ReencryptDeps;
  stored: Map<string, string>;
  putIds: string[];
  putParams: string[];
}

function recorder(failFor?: (id: string) => Error | undefined): Recorder {
  const stored = new Map<string, string>();
  const putIds: string[] = [];
  const putParams: string[] = [];
  const deps: ReencryptDeps = {
    decrypt: decryptNoteContent,
    encrypt: encryptNoteContent,
    putCipher: async (id, cipherB64, kdfParams) => {
      putIds.push(id);
      putParams.push(kdfParams);
      const failure = failFor?.(id);
      if (failure) throw failure;
      stored.set(id, cipherB64);
    },
  };
  return { deps, stored, putIds, putParams };
}

describe("verifyOldPassword", () => {
  it("只要清单里任意一条能解开就通过", async () => {
    const older = await keyFor("更早一次改密留下的密码");
    const old = await keyFor("旧密码");
    const items = [
      await sealed(older, "n-1", "更早的正文"),
      await sealed(old, "n-2", "旧密码的正文"),
    ];

    // 清单第一条不是这个密码保护的，逐条试解必须继续往下走
    expect(await verifyOldPassword(items, old, recorder().deps)).toBe(true);
  });

  it("全部解不开时返回 false", async () => {
    const other = await keyFor("别的密码");
    const old = await keyFor("旧密码");
    const items = [
      await sealed(other, "n-1", "甲"),
      await sealed(other, "n-2", "乙"),
    ];

    expect(await verifyOldPassword(items, old, recorder().deps)).toBe(false);
  });

  it("空清单时 verifyOldPassword 返回 true、runMigration 各项计数为 0", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const rec = recorder();

    expect(await verifyOldPassword([], old, rec.deps)).toBe(true);

    const report = await runMigration([], old, fresh, rec.deps);
    expect(report).toEqual({
      rows: [],
      done: 0,
      skipped: 0,
      failed: 0,
      remaining: 0,
    });
    expect(rec.putIds).toEqual([]);
  });
});

describe("planMigration", () => {
  it("把新密钥能解开的判为已迁移并排除", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(fresh, "n-1", "已迁移"),
      await sealed(old, "n-2", "待办"),
    ];

    const pending = await planMigration(items, fresh, recorder().deps);

    expect(pending.map((item) => item.id)).toEqual(["n-2"]);
  });

  it("全部已迁移时清单为空", async () => {
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(fresh, "n-1", "甲"),
      await sealed(fresh, "n-2", "乙"),
    ];

    expect(await planMigration(items, fresh, recorder().deps)).toEqual([]);
  });
});

describe("runMigration", () => {
  it("逐条用旧密钥解密、新密钥加密并提交，元数据不变", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(old, "n-1", "第一段正文"),
      await sealed(old, "n-2", "第二段正文"),
    ];
    const rec = recorder();

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(report).toEqual({
      rows: [
        { id: "n-1", outcome: "done" },
        { id: "n-2", outcome: "done" },
      ],
      done: 2,
      skipped: 0,
      failed: 0,
      remaining: 0,
    });
    expect(rec.putIds).toEqual(["n-1", "n-2"]);
    // 库里换成的新密文必须能用新密钥解开成原正文
    expect(
      await decryptNoteContent(fresh, "n-1", rec.stored.get("n-1") as string),
    ).toBe("第一段正文");
    expect(
      await decryptNoteContent(fresh, "n-2", rec.stored.get("n-2") as string),
    ).toBe("第二段正文");
  });

  it("提交的 kdf_params 默认刷成当前应用参数，可用 paramsJSON 覆盖", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [await sealed(old, "n-1", "正文")];

    const defaults = recorder();
    await runMigration(items, old, fresh, defaults.deps);
    expect(defaults.putParams).toEqual([JSON.stringify(KDF_PARAMS)]);

    const overridden = recorder();
    await runMigration(items, old, fresh, {
      ...overridden.deps,
      paramsJSON: '{"algo":"pbkdf2"}',
    });
    expect(overridden.putParams).toEqual(['{"algo":"pbkdf2"}']);
  });

  it("对已迁移的笔记不二次加密", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(fresh, "n-1", "上次跑过了"),
      await sealed(old, "n-2", "待办"),
    ];
    const rec = recorder();

    const report = await runMigration(items, old, fresh, rec.deps);

    // 中途退出续跑：新密码填进旧密码栏，已迁移那条必须原样跳过
    expect(rec.putIds).toEqual(["n-2"]);
    expect(report.rows).toEqual([
      { id: "n-1", outcome: "already_done" },
      { id: "n-2", outcome: "done" },
    ]);
    expect(report).toMatchObject({
      done: 1,
      skipped: 1,
      failed: 0,
      remaining: 0,
    });
  });

  it("跑第二遍全是 already_done，remaining 归零", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(old, "n-1", "甲"),
      await sealed(old, "n-2", "乙"),
    ];
    const rec = recorder();

    await runMigration(items, old, fresh, rec.deps);
    // 续跑前清单是从服务端重读的，密文已是新密钥的版本
    const reloaded: EncryptedItem[] = items.map((item) => ({
      id: item.id,
      cipher_b64: rec.stored.get(item.id) as string,
    }));
    const second = await runMigration(reloaded, fresh, fresh, rec.deps);

    expect(second.rows.map((row) => row.outcome)).toEqual([
      "already_done",
      "already_done",
    ]);
    expect(second).toMatchObject({
      done: 0,
      skipped: 2,
      failed: 0,
      remaining: 0,
    });
  });

  it("单条 putCipher 抛错时归类 failed_network 并继续跑完其余", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(old, "n-1", "第一段正文"),
      await sealed(old, "n-2", "第二段正文"),
    ];
    const rec = recorder((id) =>
      id === "n-1" ? new Error("请求失败（500）") : undefined,
    );

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(rec.putIds).toEqual(["n-1", "n-2"]);
    expect(report).toMatchObject({
      done: 1,
      skipped: 0,
      failed: 1,
      remaining: 1,
    });
    const failed = report.rows.find((row) => row.id === "n-1") as MigrationRow;
    expect(failed.outcome).toBe("failed_network");
    // 失败原因要能显示给用户，但正文绝不进 message
    expect(failed.message).toContain("请求失败");
    expect(JSON.stringify(report)).not.toContain("第一段正文");
  });

  it("putCipher 报 not_encrypted 时归为 skipped_gone", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(old, "n-1", "途中被删"),
      await sealed(old, "n-2", "还在"),
    ];
    const rec = recorder((id) =>
      id === "n-1"
        ? new ApiRequestError("not_encrypted", "这条笔记不是加密笔记")
        : undefined,
    );

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(report.rows).toEqual([
      { id: "n-1", outcome: "skipped_gone", message: "这条笔记不是加密笔记" },
      { id: "n-2", outcome: "done" },
    ]);
    expect(report).toMatchObject({
      done: 1,
      skipped: 1,
      failed: 0,
      remaining: 0,
    });
  });

  it("putCipher 报 not_found 时同样归为 skipped_gone", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [await sealed(old, "n-1", "途中被删")];
    const rec = recorder(() => new ApiRequestError("not_found", "笔记不存在"));

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(report.rows[0].outcome).toBe("skipped_gone");
    expect(report).toMatchObject({
      done: 0,
      skipped: 1,
      failed: 0,
      remaining: 0,
    });
  });

  it("旧密码解不开某条时归为 failed_old_key 且不影响其余条目", async () => {
    const older = await keyFor("更早的密码");
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(older, "n-1", "更早的正文"),
      await sealed(old, "n-2", "旧密码的正文"),
    ];
    const rec = recorder();

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(rec.putIds).toEqual(["n-2"]); // 解不开的那条不能被半成品覆盖
    expect(report.rows[0]).toEqual({ id: "n-1", outcome: "failed_old_key" });
    expect(report).toMatchObject({
      done: 1,
      skipped: 0,
      failed: 1,
      remaining: 1,
    });
    expect(JSON.stringify(report)).not.toContain("更早的正文");
  });

  it("remaining 等于 failed_old_key 与 failed_network 的条数", async () => {
    const older = await keyFor("更早的密码");
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(older, "n-1", "更早"),
      await sealed(old, "n-2", "会网络失败"),
      await sealed(old, "n-3", "成功"),
    ];
    const rec = recorder((id) =>
      id === "n-2" ? new Error("网络中断") : undefined,
    );

    const report = await runMigration(items, old, fresh, rec.deps);

    expect(report).toMatchObject({
      done: 1,
      skipped: 0,
      failed: 2,
      remaining: 2,
    });
  });

  it("onProgress 每条落定后回调一次，顺序与清单一致", async () => {
    const old = await keyFor("旧密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(old, "n-1", "甲"),
      await sealed(fresh, "n-2", "乙"),
    ];
    const seen: MigrationRow[] = [];

    const report = await runMigration(
      items,
      old,
      fresh,
      recorder().deps,
      (row) => {
        seen.push(row);
      },
    );

    expect(seen).toEqual(report.rows);
    expect(seen.map((row) => row.outcome)).toEqual(["done", "already_done"]);
  });
});

describe("countRemaining", () => {
  it("复核请求本身失败时退回迁移报告的计数，不抛异常", async () => {
    const fresh = await keyFor("新密码");
    const rec = recorder();
    rec.deps.decrypt = () => {
      throw new Error("Failed to fetch");
    };

    const remaining = await countRemaining(
      [await sealed(fresh, "n-1", "甲")],
      fresh,
      rec.deps,
      2,
    );

    expect(remaining).toBe(2);
  });

  it("复核成功时按新密钥实际解不开的条数计数", async () => {
    const other = await keyFor("别的密码");
    const fresh = await keyFor("新密码");
    const items = [
      await sealed(fresh, "n-1", "已迁移"),
      await sealed(other, "n-2", "没迁移"),
    ];

    expect(await countRemaining(items, fresh, recorder().deps, 0)).toBe(1);
  });
});

describe("needsPreviousPassword", () => {
  it("有 failed_old_key 时续跑要用上一个密码，新密码填进旧密码栏必然再失败", () => {
    const report: MigrationReport = {
      rows: [
        { id: "n-1", outcome: "done" },
        { id: "n-2", outcome: "failed_old_key" },
      ],
      done: 1,
      skipped: 0,
      failed: 1,
      remaining: 1,
    };

    expect(needsPreviousPassword(report)).toBe(true);
  });

  it("只有 failed_network 时新密码已经是旧密码，可以直接续跑", () => {
    const report: MigrationReport = {
      rows: [
        { id: "n-1", outcome: "failed_network", message: "Failed to fetch" },
      ],
      done: 0,
      skipped: 0,
      failed: 1,
      remaining: 1,
    };

    expect(needsPreviousPassword(report)).toBe(false);
  });
});
