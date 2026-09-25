#include <stddef.h>

typedef struct {
	char *name;
	char *addr; // 2a-6e-21-2a-b1-10
} nk_device;

// run on the main thread until nk_stop_main_loop, every other call needs it running
void nk_main_loop(void);
void nk_stop_main_loop(void);

// malloc'd, free with nk_free_devices, -1 without the main loop
int nk_paired(nk_device **out);
void nk_free_devices(nk_device *devs, int n);

// handle > 0, or 0 with *err malloc'd
int nk_open(const char *addr, char **err);
int nk_channel(int h);
// 0 ok, else *err malloc'd
int nk_write(int h, const void *p, size_t n, char **err);
// bytes read, 0 on timeout, -1 once the channel closed
int nk_read(int h, void *p, size_t n, int timeout_ms);
// 1 while the channel is open
int nk_alive(int h);
// drops unread replies
void nk_flush(int h);
void nk_close(int h);

// *ioreturn = why it stopped (0 = ran to the end, -2 = no main loop)
// stops early once a device named stop_at* shows up
int nk_inquiry(int seconds, const char *stop_at, nk_device **out, int *ioreturn);
// 0 ok, else *err malloc'd
int nk_pair(const char *addr, int timeout_s, char **err);
