-- v24 (compatible with v19+): Map Matrix emoji mxc to Discord emoji
CREATE TABLE emoticon (
    mxid    TEXT NOT NULL,
    mxc     TEXT NOT NULL,
		mxalt   TEXT NOT NULL,
    dcid    TEXT NOT NULL,
    dcname  TEXT NOT NULL,

    PRIMARY KEY (mxid, mxc)
);