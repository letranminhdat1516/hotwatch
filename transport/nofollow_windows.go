package transport

// oNoFollow is 0 on Windows, which has no such open flag; the Lstat check
// before opening refuses a symlink.
const oNoFollow = 0
