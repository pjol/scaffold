-- core: leases for periodic background jobs (jobs.Every).
--
-- A tick claims a job by moving expires_at forward, which succeeds only once
-- the previous lease has run out. The claim covers the run; when the run
-- ends, the holder moves expires_at back to the job's next due time, just
-- under one interval after the run started. So across any number of
-- instances each job runs once per interval, runs never overlap, and no
-- database connection is held while a job works.
CREATE TABLE job_leases (
    name        text PRIMARY KEY,                           -- "<module>.<job>"
    holder      text NOT NULL,                              -- instance that last claimed it: host:pid:random
    expires_at  timestamptz NOT NULL,                       -- claimable again from here
    started_at  timestamptz NOT NULL DEFAULT now(),         -- last claim
    finished_at timestamptz,                                -- NULL while running, or when the holder died mid-run
    last_error  text NOT NULL DEFAULT ''                    -- last finished run's error; '' when it succeeded
);
