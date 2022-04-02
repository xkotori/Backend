import express, { Router } from "express";
import { MongoAPIError } from "mongodb";
import { KeyManager } from "../../classes/keyManager.class";
import { DatabaseManager } from "../../database/DatabaseManager";
import { getMemoryUsage, getProcessorUsage } from "../../logic";
import { auth } from "../../middleware/auth";
import { LoggedInRequest } from "../../types/user";
import { Validators } from "../../validators";

export class V1 {
  private static HELLO_WORLD = JSON.stringify({ message: "Hello World" });
  private auth = auth(this.db);
  public router: Router = express.Router();
  public keyManager = new KeyManager();

  private constructor(public db: DatabaseManager) {
    this.router.get("/", async (req, res) => res.send(V1.HELLO_WORLD));
    this.router.get("/ping", async (req, res) => res.send());
    this.router.get("/status", async (req, res) =>
      res.json({
        memory: await getMemoryUsage(),
        processor: await getProcessorUsage(),
        uptime: process.uptime(),
      })
    );
    this.router.use("/users", this.generate_user_routes());
    this.router.use("/machines", this.generate_machine_routes());
  }

  public static create(db: DatabaseManager) {
    return new this(db).router;
  }

  private generate_user_routes() {
    const router: Router = express.Router();

    router.get("/@me", this.auth, (req: LoggedInRequest, res) => req.user!.toJSON());

    router.delete("/@me", this.auth, (req: LoggedInRequest, res) =>
      req
        .user!.delete()
        .then(() => res.send())
        .catch(() => res.status(500).send())
    );

    router.get("/@me/machines", this.auth, (req: LoggedInRequest, res) =>
      req.user!.get_machines().then((machines) => res.send(machines.map((machine) => machine.toJSON())))
    );

    router.get("/:uuid", this.auth, async (req: LoggedInRequest, res) =>
      (await this.db.find_user_by_uuid(req.params.uuid)).toJSON()
    );

    router.patch("/@avatar", this.auth, (req: LoggedInRequest, res) =>
      Validators.validate_avatar_url(req.body.url)
        ? req.user!.update_avatar(req.body.url).then((user) => res.send(user.toJSON()))
        : res.status(400).json({ error: "invalid url" })
    );

    router.patch("/@banner", this.auth, (req: LoggedInRequest, res) =>
      Validators.validate_avatar_url(req.body.url)
        ? req.user!.update_banner(req.body.url).then((user) => res.send(user.toJSON()))
        : res.status(400).json({ error: "invalid url" })
    );

    router.post("/@signup", async (req, res) =>
      this.db.new_user(req.body).then(
        ({ user, token }) => res.status(201).json({ user: user.toJSON(), token }),
        (reason) => res.status(400).json({ error: reason })
      )
    );

    router.post("/@login", async (req, res) =>
      this.db.login_user(req.body).then(
        ({ user, token }) => res.status(200).json({ user: user.toJSON(), token }),
        (reason) => res.status(400).json({ error: reason })
      )
    );

    return router;
  }

  private generate_machine_routes() {
    const router: Router = express.Router();

    router.get("/@newkey", this.auth, (req: LoggedInRequest, res) => res.json(this.keyManager.createNewKey(req.user!.uuid)));

    router.post("/@signup", async (req, res) => {
      const { two_factor_key, hardware_uuid, hostname } = req.body;
      const userUuid = this.keyManager.validate(two_factor_key);

      if (!userUuid) return res.status(403).json({ error: "the 2FA token you provided has expired" });
      this.db
        .find_user_by_uuid(userUuid)
        .then((user) => {
          this.db
            .new_machine({ owner_uuid: user.uuid, hardware_uuid, hostname })
            .then((machine) => res.json({ access_token: machine.access_token }))
            .catch((error: MongoAPIError) => {
              switch (error.code) {
                case 11000:
                  res.status(400).json({ error: "this machine is already registered in the database" });
                  break;
                default:
                  res.status(500).json({ error });
                  break;
              }
            });
        })
        .catch((error) => res.status(404).json({ error }));
    });

    router.delete("/:uuid", this.auth, async (req: LoggedInRequest, res) => {
      if (!Validators.validate_uuid(req.params.uuid)) return res.status(400).json({ error: "uuid is invalid" });
      const machine = await this.db.find_machine_by_uuid(req.params.uuid);
      if (!machine) return res.status(404).json({ error: "machine not found" });
      if (machine.owner_uuid !== req.user!.uuid)
        return res.status(403).json({ error: "you are not the owner of this machine" });
      machine
        .delete()
        .then(() => res.json({ message: "gon" }))
        .catch((error: any) => res.status(500).json({ error }));
    });

    return router;
  }
}
