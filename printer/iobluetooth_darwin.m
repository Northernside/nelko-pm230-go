//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <IOBluetooth/IOBluetooth.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>
#include "iobluetooth_darwin.h"

// IOBluetooth delivers delegate callbacks on the main run loop only
// (an inquiry started on a thread with its own run loop never completed, 2026-09-25)
// -> printer.RunMain parks the main thread in nk_main_loop and every call runs there

static atomic_bool loopRunning;

static BOOL onBT(void (^block)(void)) {
	if (NSThread.isMainThread) {
		block();
		return YES;
	}
	if (!atomic_load(&loopRunning)) {
		return NO;
	}
	// not dispatch_sync on the main queue, IOBluetooth waits on that queue itself
	// (the channel connected for ~200ms, then failed with 0xe00002bc, 2026-09-25)
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	CFRunLoopRef main = CFRunLoopGetMain();
	CFRunLoopPerformBlock(main, kCFRunLoopCommonModes, ^{
		block();
		dispatch_semaphore_signal(done);
	});
	CFRunLoopWakeUp(main);
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	return YES;
}

void nk_main_loop(void) {
	if (!NSThread.isMainThread) {
		return;
	}
	atomic_store(&loopRunning, true);
	while (atomic_load(&loopRunning)) {
		@autoreleasepool {
			CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.25, false);
		}
	}
}

void nk_stop_main_loop(void) {
	atomic_store(&loopRunning, false);
}

static const char *noLoop = "IOBluetooth needs the main thread, run the program through printer.RunMain";

@interface NKWait : NSObject
@property (strong) NSCondition *cond;
@property BOOL done;
@property IOReturn result;
- (void)finish:(IOReturn)r;
- (BOOL)waitSeconds:(double)s;
@end

@implementation NKWait
- (instancetype)init {
	if ((self = [super init])) {
		_cond = [NSCondition new];
	}
	return self;
}

- (void)finish:(IOReturn)r {
	[_cond lock];
	_result = r;
	_done = YES;
	[_cond broadcast];
	[_cond unlock];
}

- (BOOL)waitSeconds:(double)s {
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:s];
	[_cond lock];
	while (!_done && [_cond waitUntilDate:deadline]) {
	}
	BOOL done = _done;
	[_cond unlock];
	return done;
}
@end

@interface NKConn : NSObject <IOBluetoothRFCOMMChannelDelegate>
@property (strong) IOBluetoothDevice *device;
@property (strong) IOBluetoothRFCOMMChannel *channel;
@property (strong) NSMutableData *inbox;
@property (strong) NSCondition *cond; // guards the properties below
@property BOOL closed;
@property BOOL opened;
@property IOReturn openStatus;
@property int writesDone;
@property IOReturn writeStatus;
@property int channelID;
- (BOOL)waitFor:(BOOL (^)(void))cond seconds:(double)s;
@end

@implementation NKConn
- (instancetype)init {
	if ((self = [super init])) {
		_inbox = [NSMutableData data];
		_cond = [NSCondition new];
	}
	return self;
}

- (void)update:(void (^)(void))change {
	[_cond lock];
	change();
	[_cond broadcast];
	[_cond unlock];
}

// never on the main thread, the callbacks that end the wait run there
- (BOOL)waitFor:(BOOL (^)(void))cond seconds:(double)s {
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:s];
	[_cond lock];
	while (!cond() && [_cond waitUntilDate:deadline]) {
	}
	BOOL ok = cond();
	[_cond unlock];
	return ok;
}

- (void)rfcommChannelOpenComplete:(IOBluetoothRFCOMMChannel *)ch status:(IOReturn)error {
	[self update:^{
		self.opened = YES;
		self.openStatus = error;
	}];
}

- (void)rfcommChannelWriteComplete:(IOBluetoothRFCOMMChannel *)ch refcon:(void *)refcon status:(IOReturn)error {
	[self update:^{
		self.writesDone++;
		if (error != kIOReturnSuccess) {
			self.writeStatus = error;
		}
	}];
}

- (void)rfcommChannelData:(IOBluetoothRFCOMMChannel *)ch data:(void *)data length:(size_t)length {
	[self update:^{
		[self.inbox appendBytes:data length:length];
	}];
}

- (void)rfcommChannelClosed:(IOBluetoothRFCOMMChannel *)ch {
	[self update:^{
		self.closed = YES;
	}];
}
@end

static NSMutableDictionary<NSNumber *, NKConn *> *conns;
static int nextHandle = 1;

static NKConn *lookup(int h) {
	@synchronized ([NKConn class]) {
		return conns[@(h)];
	}
}

static char *errorf(NSString *format, ...) NS_FORMAT_FUNCTION(1, 2);
static char *errorf(NSString *format, ...) {
	va_list args;
	va_start(args, format);
	NSString *s = [[NSString alloc] initWithFormat:format arguments:args];
	va_end(args);
	return strdup(s.UTF8String);
}

int nk_paired(nk_device **out) {
	__block int n = 0;
	__block nk_device *devs = NULL;
	BOOL ran = onBT(^{
		NSArray *paired = [IOBluetoothDevice pairedDevices];
		devs = calloc(paired.count ? paired.count : 1, sizeof(nk_device));
		for (IOBluetoothDevice *d in paired) {
			devs[n].name = strdup((d.name ?: @"").UTF8String);
			devs[n].addr = strdup((d.addressString ?: @"").UTF8String);
			n++;
		}
	});
	*out = devs;
	return ran ? n : -1;
}

void nk_free_devices(nk_device *devs, int n) {
	for (int i = 0; i < n; i++) {
		free(devs[i].name);
		free(devs[i].addr);
	}
	free(devs);
}

@interface NKSDP : NKWait
@end

@implementation NKSDP
- (void)sdpQueryComplete:(IOBluetoothDevice *)device status:(IOReturn)status {
	[self finish:status];
}
@end

static IOBluetoothSDPServiceRecord *sppRecord(IOBluetoothDevice *dev) {
	return [dev getServiceRecordForUUID:[IOBluetoothSDPUUID uuid16:kBluetoothSDPUUID16ServiceClassSerialPort]];
}

int nk_open(const char *addr, char **err) {
	NSString *address = [NSString stringWithUTF8String:addr];
	__block IOBluetoothDevice *dev = nil;
	__block BOOL cached = NO;
	__block IOReturn sdpStarted = kIOReturnError;
	NKSDP *sdp = [NKSDP new];
	BOOL ran = onBT(^{
		dev = [IOBluetoothDevice deviceWithAddressString:address];
		cached = dev && sppRecord(dev);
		if (dev && !cached) {
			sdpStarted = [dev performSDPQuery:sdp]; // nothing cached right after pairing
		}
	});
	if (!ran) {
		*err = strdup(noLoop);
		return 0;
	}
	if (!dev) {
		*err = errorf(@"%@ is not a bluetooth address", address);
		return 0;
	}
	if (!cached && sdpStarted == kIOReturnSuccess) {
		[sdp waitSeconds:15];
	}

	// the Sync calls block the main thread, one hung writeSync for good (2026-09-25)
	// -> async calls, waited for here off the main thread
	NKConn *conn = [NKConn new];
	conn.device = dev;
	__block IOReturn started = kIOReturnError;
	__block BOOL haveRecord = NO;
	onBT(^{
		// SPP record (UUID 0x1101) names the channel, 1 by convention
		BluetoothRFCOMMChannelID channelID = 1;
		IOBluetoothSDPServiceRecord *record = sppRecord(dev);
		if (record) {
			[record getRFCOMMChannelID:&channelID];
			haveRecord = YES;
		}
		conn.channelID = channelID;
		IOBluetoothRFCOMMChannel *channel = nil;
		started = [dev openRFCOMMChannelAsync:&channel withChannelID:channelID delegate:conn];
		conn.channel = channel;
	});
	if (started != kIOReturnSuccess || !conn.channel) {
		*err = errorf(@"opening RFCOMM channel %d on %@ did not start (IOReturn 0x%08x, SPP record %@)",
			conn.channelID, address, started, haveRecord ? @"found" : @"missing");
		return 0;
	}
	BOOL opened = [conn waitFor:^BOOL { return conn.opened || conn.closed; } seconds:20];
	if (!opened || conn.openStatus != kIOReturnSuccess || conn.closed) {
		*err = opened
			? errorf(@"opening RFCOMM channel %d on %@ failed (IOReturn 0x%08x)", conn.channelID, address, conn.openStatus)
			: errorf(@"opening RFCOMM channel %d on %@ timed out", conn.channelID, address);
		onBT(^{
			[conn.channel setDelegate:nil];
			[conn.channel closeChannel];
		});
		return 0;
	}
	int handle;
	@synchronized ([NKConn class]) {
		if (!conns) {
			conns = [NSMutableDictionary dictionary];
		}
		handle = nextHandle++;
		conns[@(handle)] = conn;
	}
	return handle;
}

int nk_channel(int h) {
	NKConn *conn = lookup(h);
	return conn ? conn.channelID : 0;
}

int nk_write(int h, const void *p, size_t n, char **err) {
	NKConn *conn = lookup(h);
	if (!conn) {
		*err = errorf(@"channel %d is closed", h);
		return -1;
	}
	NSData *data = [NSData dataWithBytes:p length:n]; // writeAsync reads it after the Go buffer is gone
	__block size_t mtu = 0;
	onBT(^{
		mtu = conn.channel.getMTU;
	});
	if (mtu == 0) {
		mtu = 127; // RFCOMM default frame size
	}
	for (size_t off = 0; off < n;) {
		UInt16 chunk = (UInt16)MIN(mtu, n - off);
		__block int want = 0;
		__block IOReturn r = kIOReturnError;
		BOOL ran = onBT(^{
			want = conn.writesDone + 1;
			r = [conn.channel writeAsync:(void *)((const char *)data.bytes + off) length:chunk refcon:NULL];
		});
		if (!ran) {
			*err = strdup(noLoop);
			return -1;
		}
		if (r != kIOReturnSuccess) {
			*err = errorf(@"write failed after %zu of %zu bytes (IOReturn 0x%08x)", off, n, r);
			return -1;
		}
		if (![conn waitFor:^BOOL { return conn.writesDone >= want || conn.closed; } seconds:10]) {
			*err = errorf(@"write stuck after %zu of %zu bytes, the printer stopped taking data", off, n);
			return -1;
		}
		if (conn.closed || conn.writeStatus != kIOReturnSuccess) {
			*err = errorf(@"write failed after %zu of %zu bytes (IOReturn 0x%08x%@)", off, n, conn.writeStatus, conn.closed ? @", channel closed" : @"");
			return -1;
		}
		off += chunk;
	}
	return 0;
}

int nk_read(int h, void *p, size_t n, int timeout_ms) {
	NKConn *conn = lookup(h);
	if (!conn) {
		return -1;
	}
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:timeout_ms / 1000.0];
	[conn.cond lock];
	while (conn.inbox.length == 0 && !conn.closed) {
		if (![conn.cond waitUntilDate:deadline]) {
			break;
		}
	}
	int got = 0;
	if (conn.inbox.length > 0) {
		got = (int)MIN(n, conn.inbox.length);
		memcpy(p, conn.inbox.bytes, got);
		[conn.inbox replaceBytesInRange:NSMakeRange(0, got) withBytes:NULL length:0];
	} else if (conn.closed) {
		got = -1;
	}
	[conn.cond unlock];
	return got;
}

int nk_alive(int h) {
	NKConn *conn = lookup(h);
	if (!conn) {
		return 0;
	}
	[conn.cond lock];
	BOOL closed = conn.closed;
	[conn.cond unlock];
	return closed ? 0 : 1;
}

void nk_flush(int h) {
	NKConn *conn = lookup(h);
	if (!conn) {
		return;
	}
	[conn.cond lock];
	conn.inbox.length = 0;
	[conn.cond unlock];
}

void nk_close(int h) {
	NKConn *conn;
	@synchronized ([NKConn class]) {
		conn = conns[@(h)];
		[conns removeObjectForKey:@(h)];
	}
	if (!conn) {
		return;
	}
	onBT(^{
		[conn.channel closeChannel];
	});
	// the baseband is left to macOS, dropping it too broke the next open (2026-09-25)
	[conn waitFor:^BOOL { return conn.closed; } seconds:3];
	onBT(^{
		[conn.channel setDelegate:nil];
	});
}


@interface NKInquiry : NKWait <IOBluetoothDeviceInquiryDelegate>
@property (strong) IOBluetoothDeviceInquiry *inquiry;
@property (copy) NSString *stopAt;
@end

@implementation NKInquiry
- (void)check:(IOBluetoothDeviceInquiry *)sender device:(IOBluetoothDevice *)d {
	if ([d.name hasPrefix:self.stopAt]) {
		[sender stop]; // the other names nearby took 30s to resolve
		[self finish:kIOReturnSuccess];
	}
}

- (void)deviceInquiryDeviceFound:(IOBluetoothDeviceInquiry *)sender device:(IOBluetoothDevice *)d {
	[self check:sender device:d];
}

- (void)deviceInquiryDeviceNameUpdated:(IOBluetoothDeviceInquiry *)sender device:(IOBluetoothDevice *)d devicesRemaining:(uint32_t)n {
	[self check:sender device:d];
}
- (void)deviceInquiryComplete:(IOBluetoothDeviceInquiry *)sender error:(IOReturn)error aborted:(BOOL)aborted {
	[self finish:error];
}
@end

int nk_inquiry(int seconds, const char *stop_at, nk_device **out, int *ioreturn) {
	NKInquiry *q = [NKInquiry new];
	q.stopAt = [NSString stringWithUTF8String:stop_at];
	BOOL ran = onBT(^{
		q.inquiry = [IOBluetoothDeviceInquiry inquiryWithDelegate:q];
		q.inquiry.inquiryLength = (uint8_t)seconds;
		q.inquiry.updateNewDeviceNames = YES;
		IOReturn r = [q.inquiry start];
		if (r != kIOReturnSuccess) {
			[q finish:r];
		}
	});
	if (!ran) {
		*out = calloc(1, sizeof(nk_device));
		*ioreturn = -2;
		return 0;
	}
	BOOL finished = [q waitSeconds:seconds + 20]; // name requests follow the inquiry
	*ioreturn = finished ? q.result : kIOReturnTimeout;

	__block int n = 0;
	__block nk_device *devs = NULL;
	onBT(^{
		[q.inquiry stop];
		NSArray *found = q.inquiry.foundDevices;
		devs = calloc(found.count ? found.count : 1, sizeof(nk_device));
		for (IOBluetoothDevice *d in found) {
			devs[n].name = strdup((d.name ?: @"").UTF8String);
			devs[n].addr = strdup((d.addressString ?: @"").UTF8String);
			n++;
		}
		q.inquiry.delegate = nil;
	});
	*out = devs;
	return n;
}

@interface NKPair : NKWait
@property (strong) IOBluetoothDevicePair *pair;
@end

@implementation NKPair
- (void)devicePairingPINCodeRequest:(id)sender {
	BluetoothPINCode pin;
	memset(&pin, 0, sizeof pin);
	memcpy(pin.data, "0000", 4); // a guess, the manual names no PIN
	[sender replyPINCode:4 PINCode:&pin];
}

- (void)devicePairingUserConfirmationRequest:(id)sender numericValue:(BluetoothNumericValue)numericValue {
	[sender replyUserConfirmation:YES]; // no screen on the printer to compare against
}

- (void)devicePairingFinished:(id)sender error:(IOReturn)error {
	[self finish:error];
}
@end

int nk_pair(const char *addr, int timeout_s, char **err) {
	NSString *address = [NSString stringWithUTF8String:addr];
	NKPair *p = [NKPair new];
	__block char *failure = NULL;
	__block BOOL already = NO;
	BOOL ran = onBT(^{
		IOBluetoothDevice *dev = [IOBluetoothDevice deviceWithAddressString:address];
		if (!dev) {
			failure = errorf(@"%@ is not a bluetooth address", address);
			return;
		}
		if (dev.isPaired) {
			already = YES;
			return;
		}
		p.pair = [IOBluetoothDevicePair pairWithDevice:dev];
		p.pair.delegate = p;
		IOReturn r = [p.pair start];
		if (r != kIOReturnSuccess) {
			failure = errorf(@"pairing %@ did not start (IOReturn 0x%08x)", address, r);
		}
	});
	if (!ran) {
		failure = strdup(noLoop);
	}
	if (failure) {
		*err = failure;
		return -1;
	}
	if (already) {
		return 0;
	}
	BOOL finished = [p waitSeconds:timeout_s];
	onBT(^{
		if (!finished) {
			[p.pair stop];
		}
		p.pair.delegate = nil;
	});
	if (!finished) {
		*err = errorf(@"pairing %@ timed out after %ds", address, timeout_s);
		return -1;
	}
	if (p.result != kIOReturnSuccess) {
		*err = errorf(@"pairing %@ failed (IOReturn 0x%08x)", address, p.result);
		return -1;
	}
	return 0;
}
