#include <queue>

#include <WiFi.h>
#include <WiFiUdp.h>
#include <WebServer.h>  // https://github.com/espressif/arduino-esp32
#include <MDNS_Generic.h>
#include <esp_sleep.h>
#include "driver/rtc_io.h"
#include <esp_pm.h>

// Generate a uniformly distributed random number, given a mean and
// a variance. The number will be in the range [mean - var, mean + var),
// but will always be at least 0.
static int generate_random(int mean, int var) {
  // Generate a random number in the range [-1.0, 1.0).
  const float scale = 65536.0;
  float rand = static_cast<float>(random(scale * 2)) / scale - 1.0;

  float j = static_cast<float>(jitter) * rand;
  int d = delay + static_cast<int>(j);
  if (d < 0) {
    d = 0;
  }
  return d;
}

// ------------------------------------------------------------------

class Net {
 public:
  Net(const String& ssid, const String& pass, int port, bool debug_enabled) :
    ssid_(ssid), pass_(pass), debug_enabled_(debug_enabled),
    mdns_(udp_), server_(port) {}

  void setup() {
    debugln("\nConnecting to WiFi:");
    WiFi.mode(WIFI_STA);
    WiFi.begin(ssid_, pass_);
    while (WiFi.status() != WL_CONNECTED) {
      char buf[80];
      wifi_status(buf, sizeof (buf));
      debug("  ");
      debug(buf);
      delay(500);
    }
    debug("Done connecting. Address is ");
    debugln(WiFi.localIP());

    debugln("Registering hostname and service with mDNS");
    char hostname[80];
    snprintf(hostname, sizeof (hostname), "cricket_%016llx", get_mac());
    mdns_.begin(WiFi.localIP(), hostname);
    char service[80];
    snprintf(service, sizeof (service), "Cricket %016llx._http", get_mac());
    mdns_.addServiceRecord(service, 80, MDNSServiceTCP);
    debug("http://");
    debug(hostname);
    debugln(".local/");

    server_.begin();
    // Without this, the server calls delay(1) every loop()!
    server_.enableDelay(false);

    on("/wifi", [this]() {
      char buf[80];
      wifi_status(buf, sizeof (buf));
      sendSuccess(String(buf));
    });
  }

  void on(const Uri &uri, WebServer::THandlerFunction handler) {
    server_.on(uri, HTTP_ANY, handler);
  }

  void sendSuccess(const String& msg = "") {
    if (msg != "") {
      server_.send(200, "text/plain", msg + "\n");
    } else {
      server_.send(200, "text/plain", "");
    }
  }

  void sendFailure(const String& msg = "") {
    if (msg != "") {
      server_.send(401, "text/plain", msg + "\n");
    } else {
      server_.send(401, "text/plain", "");
    }
  }

  bool hasArg(String name) {
    return server_.hasArg(name);
  }

  String arg(String name) {
    return server_.arg(name);
  }

  uint64_t get_mac() {
    uint64_t mac = esp_.getEfuseMac();
    // swap the endianness of the returned value
    mac = (mac & 0x00000000ffffffff) << 32 | (mac & 0xffffffff00000000) >> 32;
    mac = (mac & 0x0000ffff0000ffff) << 16 | (mac & 0xffff0000ffff0000) >> 16;
    mac = (mac & 0x00ff00ff00ff00ff) << 8  | (mac & 0xff00ff00ff00ff00) >> 8;
    return mac;
  }

  void loop() {
    mdns_.run();
    server_.handleClient();
  }

 private:
  template <typename T> void debug(T t) {
    if (debug_enabled_) Serial.print(t);
  }
  template <typename T> void debugln(T t) {
    if (debug_enabled_) Serial.println(t);
  }

  void wifi_status(char *buf, size_t len) {
    // from https://forum.arduino.cc/t/486265/5:
    //
    // if wifi is visible:      WiFi.begin() >> DISCONNECTED >> CONNECTED
    // if wifi then disappears:                 DISCONNECTED >> NO_SSID_AVAIL
    // 
    // if wifi is not visible:  WiFi.begin() >> DISCONNECTED >> NO_SSID_AVAIL
    // if wifi then appears:                    DISCONNECTED >> CONNECTED
    // 
    // if disconnecting wifi:   WiFi.disconnect() >> IDLE_STATUS
    // 
    // if wifi configuration is wrong (e.g. wrong password):
    //                          WiFi.begin() >> DISCONNECTED >> CONNECT_FAILED
    const char *status = "";
    switch (WiFi.status()) {
      // A temporary status assigned when WiFi.begin() is called,
      // and remains active until
      // - the number of attempts expires (resulting in WL_CONNECT_FAILED), or
      // - a connection is established (resulting in WL_CONNECTED)
      case WL_IDLE_STATUS:     status = "Idle    "; break; // 0

      // No SSIDs are available
      case WL_NO_SSID_AVAIL:   status = "NoSSID  "; break; // 1

      // Scan of networks is completed
      case WL_SCAN_COMPLETED:  status = "ScanDone"; break; // 2

      // Connected to a WiFi network
      case WL_CONNECTED:       status = "Connect "; break; // 3

      // Connection failed for all the attempts
      case WL_CONNECT_FAILED:  status = "ConnFail"; break; // 4

      // The connection is lost
      case WL_CONNECTION_LOST: status = "ConnLost"; break; // 5

      // Disconnected from a network
      case WL_DISCONNECTED:    status = "Disconn "; break; // 6
    }

    snprintf(buf, len, "WiFi RSSI: %d status: %8s\n", WiFi.RSSI(), status);
  }

  String ssid_;
  String pass_;
  bool debug_enabled_;

  WiFiUDP udp_;
  MDNS mdns_;
  WebServer server_;
  EspClass esp_;
};

// ---------------------------------------------------------------------
// DFPlayerImpl actually does actions to the DFPlayer.

class DFPlayerImpl {
 public:
  // The pins here are pins on the ESP32.
  DFPlayerImpl(byte tx_pin, byte rx_pin, byte busy_pin) :
      tx_pin_(tx_pin), rx_pin_(rx_pin), busy_pin_(busy_pin), serial_(1) {
    pinMode(busy_pin_, INPUT);
  }

  void init() {
    pinMode(rx_pin_, OUTPUT);
    pinMode(tx_pin_, OUTPUT);
    serial_.begin(9600, SERIAL_8N1, rx_pin_, tx_pin_);
  }

  void fini() {
    // Move these pins into high-impedance mode, so they don't accidentally
    // leak any current that the DFPlayer could snack on.
    pinMode(rx_pin_, INPUT);
    pinMode(tx_pin_, INPUT);
    serial_.end();
  }

  bool is_busy() {
    return (digitalRead(busy_pin_) == LOW);
  }

  void drain_serial() {
    for (int i = 0; i < kPacketSize; i++) {
      while (!serial_.available()) {}
      (void) serial_.read();
    }
  }

  void send_cmd(byte cmd, byte param1, byte param2) {
    const byte kStartByte     = 0x7E;
    const byte kVersionByte   = 0xFF;
    const byte kCommandLength = 0x06;
    const byte kAcknowledge   = 0x00;
    const byte kEndByte       = 0xEF;

    uint16_t checksum =
      -(kVersionByte + kCommandLength + cmd + kAcknowledge + param1 + param2);

    byte packet[kPacketSize] = {
      kStartByte,
      kVersionByte,
      kCommandLength,
      cmd,
      kAcknowledge,
      param1,
      param2,
      highByte(checksum),
      lowByte(checksum),
      kEndByte
    };

    for (byte i = 0; i < kPacketSize; i++) serial_.write(packet[i]);
  }

 private:
  const int kPacketSize = 10;

  byte tx_pin_;
  byte rx_pin_;
  byte busy_pin_;
  HardwareSerial serial_;
};

// ---------------------------------------------------------------------
// An action that should be taken at a specified point.

class DelayedAction {
 public:
  void invoke_when_ready(const std::function<bool(void)>& ready,
      const std::function<void(void)>& func_to_invoke) {
    ready_ = ready;
    fn_ = func_to_invoke;
  }

  void invoke_after(int delay_msec,
      const std::function<void(void)>& func_to_invoke) {
    const int deadline = millis() + delay_msec;
    invoke_when_ready(
      [deadline]() { return millis() >= deadline; }, func_to_invoke);
  }

  void act() {
    if (!ready_()) return;
    std::function<void(void)> call = fn_;
    ready_ = nullptr;
    fn_ = nullptr;
    call();
  }

  bool pending() { return ready_ != nullptr; }

  void cancel() {
    ready_ = nullptr;
    fn_ = nullptr;
  }

 private:
  std::function<bool(void)> ready_;
  std::function<void(void)> fn_;
};

// ---------------------------------------------------------------------
// A version of DFPlayer that executes its commands asynchronously.
// It relies on its owner to invoke loop() periodically to make progress.

class DFPlayerAsync {
 public:
  DFPlayerAsync(byte tx_pin, byte rx_pin, byte busy_pin, bool debug_enabled) :
      dfplayer_(tx_pin, rx_pin, busy_pin), debug_enabled_(debug_enabled) {}

  // Call this after power is provided to DFPlayer.
  void enqueue_init() {
    debugln("pushing: DFPlayer init");
    work_queue_.push([this]() { do_init_step1(); });
    loop();
  }

  bool enqueue_volume(byte level) {
    char buf[80];
    snprintf(buf, sizeof (buf), "pushing: volume %d", level);
    debugln(buf);
    if (level > 0x30) return false;
    work_queue_.push([this, level]() { do_volume(level); });
    loop();
    return true;
  }

  void enqueue_play_file(byte folder, byte file) {
    char buf[80];
    snprintf(buf, sizeof (buf), "pushing: play %d/%d", folder, file);
    debugln(buf);
    work_queue_.push([this, folder, file]() { do_play_file(folder, file); });
    loop();
  }

  void enqueue_pause() {
    debugln("pushing: pause");
    work_queue_.push([this]() { do_pause(); });
    loop();
  }

  void enqueue_unpause() {
    debugln("pushing: unpause");
    work_queue_.push([this]() { do_unpause(); });
    loop();
  }

  void enqueue_stop() {
    debugln("pushing: stop");
    work_queue_.push([this]() { do_stop(); });
    loop();
  }

  // Indicates how much work there is to be done.
  unsigned int work_pending() {
    return work_queue_.size() + (action_.pending() ? 1 : 0);
  }

  // Call this when power is about to be removed.
  // This executes synchronously.
  void fini() {
    debugln("DFPlayer fini");
    action_.cancel();
    dfplayer_.fini();
    drain_work_queue();
  }

  // Take an action, if possible.
  void loop() {
    if (action_.pending()) {
      action_.act();
      return;
    }
    if (!work_queue_.empty()) {
      debugln("popping");
      std::function<void(void)> fn = work_queue_.front();
      work_queue_.pop();
      fn();
    }
  }

 private:
  void debugln(String s) {
    if (debug_enabled_) Serial.println(s);
  }

  const int kPostSerialInitDelay = 1000; // XXX probably don't need this much
  const int kPostCommandDelay = 30; // 20 is not enough (for volume at least)

  // If "busy" is not set for at least this long after playing a file,
  // it will look for non-busy and then busy again.
  // XXX: this will cause a hang if any files are <200 msec
  const int kMinBusyDelay = 200;

  void do_init_step1() {
    debugln("running: init_step1 (start serial comms)");
    dfplayer_.init();
    action_.invoke_after(kPostSerialInitDelay, [this]() { do_init_step2(); });
  }

  void do_init_step2() {
    debugln("running: init_step2 (send init params, wait for reply)");
    // Send request for initialization parameters, and discard them.
    dfplayer_.send_cmd(0x3f, 0x00, 0x00);
    dfplayer_.drain_serial();
    post_command_delay();
  }

  void do_volume(byte level) {
    char message[80];
    snprintf(message, sizeof (message), "running: volume %d", level);
    debugln(message);
    dfplayer_.send_cmd(0x06, 0x00, level);
    post_command_delay();
  }

  void do_play_file(byte folder, byte file) {
    char message[80];
    snprintf(message, sizeof (message), "running: play_file %d/%d", folder, file);
    debugln(message);
    // This assumes the DFPlayer is in file mode #2 (microSD card 
    // with directories 01-99, with filenames 001.mp3-255.mp3).
    dfplayer_.send_cmd(0x0f, folder, file);
    wait_for_busy_to_set();
  }

  void do_pause() {
    debugln("running: pause");
    dfplayer_.send_cmd(0x1a, 0, 1);
    post_command_delay();
  }

  void do_unpause() {
    debugln("running: unpause");
    dfplayer_.send_cmd(0x1a, 0, 0);
    post_command_delay();
  }

  void do_stop() {
    debugln("running: stop");
    dfplayer_.send_cmd(0x16, 0, 0);
    post_command_delay();
  }

  void post_command_delay() {
    action_.invoke_after(kPostCommandDelay, [this]() { loop(); });
  }

  void wait_for_busy_to_set() {
    debugln("running: wait_for_busy_to_set");
    action_.invoke_when_ready(
      /* ready */ [this]() { return dfplayer_.is_busy(); },
      /* call  */ [this]() { wait_for_busy_to_clear(); });
  }

  void wait_for_busy_to_clear() {
    debugln("running: wait_for_busy_to_clear");
    int deadline = millis() + kMinBusyDelay;
    action_.invoke_when_ready(
      /* ready */ [this]() { return !dfplayer_.is_busy(); },
      /* call  */ [this, deadline]() { busy_is_now_clear(deadline); });
  }

  void busy_is_now_clear(int deadline) {
    debugln("running: busy_is_now_clear");
    if (millis() < deadline) {
      // Busy was cleared too quickly; try again.
      wait_for_busy_to_set();
    } else {
      loop();
    }
  }

  void drain_work_queue() {
    std::queue<std::function<void(void)>>().swap(work_queue_);
  }

  DFPlayerImpl dfplayer_;
  std::queue<std::function<void(void)>> work_queue_;
  DelayedAction action_;
  bool debug_enabled_;
};

// ---------------------------------------------------------------------
// Mosfet controls power to the DFPlayer.

class Mosfet {
 public:
  Mosfet(byte pin, gpio_num_t gpio_num) : pin_(pin), gpio_num_(gpio_num) {
    off();
  }

  void on() {
    // Setting this to INPUT moves it into high-impedance mode, which allows
    // the resistor connected to ground to pull the voltage down to 0, which
    // in turn switches on the MOSFET.
    pinMode(pin_, INPUT);
  }

  void off() {
    // Setting this to OUTPUT, and writing a HIGH to it, allows the 3.3V signal
    // to reach the MOSFET gate. This turns the MOSFET off, since that 3.3V
    // gate voltage is equal to the MOSFET's source voltage.
    pinMode(pin_, OUTPUT);
    digitalWrite(pin_, HIGH);
  }

  // Prepare for going to sleep - indicate that this pin should be left in its
  // current state.
  void sleep_enter() {
    gpio_hold_en(gpio_num_);
  }

  // Return from sleep.
  void sleep_exit() {
    gpio_hold_dis(gpio_num_);
  }

 private:
  byte pin_;
  gpio_num_t gpio_num_;
};

// ------------------------------------------------------------------
// Firefly runs the LED.

class Firefly {
 public:
  Firefly(byte pin) : pin_(pin) {
    pinMode(pin_, OUTPUT);
  }

  void add_blink(float speed, int delay, int jitter, int reps) {
    if (speed < 0.01) {
      speed = 0.01;
    } else if (speed >= 255.0) {
      speed = 255.0;
    }
    if (delay < 0) {
      delay = 0;
    }
    if (jitter < 0) {
      jitter = 0;
    }

    blinks_.push(BlinkSet {
      .speed = speed,
      .delay = delay,
      .jitter = jitter,
      .reps = reps,
      .sign = 1,
      .counter = 0.0,
      .delay_counter = 0,
      .pwm_value = 0,
    });
  }

  unsigned int work_pending() {
    return blinks_.size() + (b_.reps > 0 ? 1 : 0);
  }

  void loop() {
    // Current blink set is done.
    if (b_.reps <= 0) {
      if (blinks_.empty()) return;
      b_ = blinks_.front();
      blinks_.pop();
    }

    // Delay between blinks in a set.
    if (b_.sign == 0) {
      if (b_.delay_counter == 0) {
        --b_.reps;
        b_.sign = 1;
      } else {
        --b_.delay_counter;
      }
      return;
    }

    float new_counter = b_.counter + b_.speed * b_.sign;
    if (static_cast<int>(new_counter) == b_.pwm_value) {
      // No PWM update needed.
      b_.counter = new_counter;
      return;
    }

    if (new_counter >= 256.0) {
      // Brightness has hit max; time to decrease.
      b_.sign = -1;
      new_counter = b_.counter - b_.speed;
    } else if (new_counter < 0.0) {
      // Brightness has hit min; shut off light and pause.
      b_.sign = 0;
      new_counter = 0.0;
      b_.delay_counter = generate_random(b_.delay, b_.jitter);
    }
    b_.counter = new_counter;
    b_.pwm_value = static_cast<int>(new_counter);
    analogWrite(pin_, b_.pwm_value);
  }

 private:
  struct BlinkSet {
    float speed; // how much to increment the PWM value each time
    int delay;   // how much to wait between blinks
    int jitter;  // the max amount that "delay" can vary each time
    int reps;    // how many times to blink it

    int sign;      // +1 for brighter, -1 for dimmer, 0 for delay
    float counter;
    int delay_counter;
    int pwm_value;
  };

  byte pin_;
  std::queue<BlinkSet> blinks_;
  BlinkSet b_;
};

// ------------------------------------------------------------------
// Battery runs the battery meter.

class Battery {
 public:
  Battery(byte pin) : pin_(pin) {
    pinMode(pin_, INPUT);
  }

  float read_voltage() {
    const int kNumReadings = 16;

    uint32_t voltage = 0;
    // Read using the ADC, using its builtin correction.
    for (int i = 0; i < kNumReadings; i++) {
      voltage += analogReadMilliVolts(pin_);
    }

    // It's hooked up to a voltage divider which cuts the voltage in half,
    // and it reads in millivolts.
    return static_cast<float>(voltage) * 2.0 / 1000.0 /
      static_cast<float>(kNumReadings);
  }

 private:
  byte pin_;
};

// ---------------------------------------------------------------------
// Cricket runs the whole device.

struct CricketConfig {
  byte dfplayer_tx_pin;
  byte dfplayer_rx_pin;
  byte dfplayer_busy_pin;
  byte mosfet_pin;
  gpio_num_t mosfet_gpio_num;
  byte firefly_pin;
  byte battery_pin;

  int shutdown_delay_msec;
  int initial_volume;
  bool debug_enabled;

  String ssid;
  String pass;
  int port;
};

class Cricket {
 public:
  Cricket(const CricketConfig& config) :
      net_(config.ssid, config.pass, config.port, config.debug_enabled),
      dfplayer_(config.dfplayer_tx_pin, config.dfplayer_rx_pin,
        config.dfplayer_busy_pin, config.debug_enabled),
      mosfet_(config.mosfet_pin, config.mosfet_gpio_num),
      firefly_(config.firefly_pin),
      battery_(config.battery_pin),
      volume_(config.initial_volume),
      shutdown_delay_msec_(config.shutdown_delay_msec),
      debug_enabled_(config.debug_enabled) {}

  void setup() {
    net_.setup();

    net_.on("/ping", [this]() {
      ping();
      net_.sendSuccess();
    });

    net_.on("/play", [this]() {
      char msg[80];
      int folder = net_.arg("folder").toInt();
      int file = net_.arg("file").toInt();
      if (folder < 1 || folder > 99) {
        snprintf(msg, sizeof (msg), "folder %d must be between 1 and 99 inclusive", folder);
        net_.sendFailure(msg);
      } else if (file < 1 || file > 255) {
        snprintf(msg, sizeof (msg), "file %d must be between 1 and 255 inclusive", file);
        net_.sendFailure(msg);
      } else {
        play(folder, file);
        // the server code expects the volume to immediately follow a colon
        snprintf(msg, sizeof (msg), "playing at volume:%d", volume_);
        net_.sendSuccess(msg);
      }
    });

    net_.on("/setvolume", [this]() {
      int vol = net_.arg("volume").toInt();
      String persist = net_.arg("persist");
      if (vol < 0 || vol > 48) {
        net_.sendFailure("volume must be between 0 and 48 inclusive");
      } else if (persist != "" && persist != "true" && persist != "false") {
        net_.sendFailure("persist must be either \"true\" or \"false\"");
      } else {
        if (!set_volume(vol, persist == "true")) {
          net_.sendFailure();
        } else {
          net_.sendSuccess();
        }
      }
    });

    net_.on("/blink", [this]() {
      float speed = net_.arg("speed").toFloat();
      int delay = net_.arg("delay").toInt();
      int jitter = net_.arg("jitter").toInt();
      int reps = net_.arg("reps").toInt();
      if (speed < 0.001) {
        net_.sendFailure("speed must be faster");
      } else if (reps <= 0) {
        net_.sendFailure("reps must be a positive number");
      } else {
        add_blink(speed, delay, jitter, reps);
        net_.sendSuccess();
      }
    });

    net_.on("/pause", [this]() {
      pause();
      net_.sendSuccess();
    });

    net_.on("/unpause", [this]() {
      unpause();
      net_.sendSuccess();
    });

    net_.on("/stop", [this]() {
      stop();
      net_.sendSuccess();
    });

    net_.on("/battery", [this]() {
      net_.sendSuccess(String(read_battery_voltage()));
    });

    net_.on("/soundpending", [this]() {
      net_.sendSuccess(String(sound_pending()));
    });

    net_.on("/lightpending", [this]() {
      net_.sendSuccess(String(light_pending()));
    });
  }

  void loop() {
    net_.loop();
    dfplayer_.loop();
    firefly_.loop();

    if (dfplayer_.work_pending()) {
      dfplayer_extend_lifetime();
    }
    if (dfplayer_should_power_off()) {
      dfplayer_power_off();
    }
  }

  void ping() {
    debugln("cricket: ping");
    dfplayer_extend_lifetime();
  }

  // this only plays async
  void play(int folder, int file) {
    dfplayer_ensure_powered_on();
    dfplayer_.enqueue_play_file(folder, file);
    dfplayer_extend_lifetime();
  }

  bool set_volume(int volume, bool persist) {
    dfplayer_ensure_powered_on();
    if (!dfplayer_.enqueue_volume(volume)) return false;
    if (persist) volume_ = volume;
    dfplayer_extend_lifetime();
    return true;
  }

  void add_blink(float speed, int delay, int jitter, int reps) {
    debugln("cricket: adding blink to queue");
    firefly_.add_blink(speed, delay, jitter, reps);
  }

  void pause() {
    dfplayer_ensure_powered_on();
    dfplayer_.enqueue_pause();
    dfplayer_extend_lifetime();
  }

  void unpause() {
    dfplayer_ensure_powered_on();
    dfplayer_.enqueue_unpause();
    dfplayer_extend_lifetime();
  }

  void stop() {
    dfplayer_ensure_powered_on();
    dfplayer_.enqueue_stop();
    dfplayer_extend_lifetime();
  }

  float read_battery_voltage() {
    return battery_.read_voltage();
  }

  unsigned int sound_pending() {
    return dfplayer_.work_pending();
  }
  unsigned int light_pending() {
    return firefly_.work_pending();
  }

  void sleep_enter() {
    mosfet_.sleep_enter();
  }
  void sleep_exit() {
    mosfet_.sleep_exit();
  }

 private:
  template <typename T> void debug(T t) {
    if (debug_enabled_) Serial.print(t);
  }
  template <typename T> void debugln(T t) {
    if (debug_enabled_) Serial.println(t);
  }

  void dfplayer_ensure_powered_on() {
    if (dfplayer_powered_on()) return;

    mosfet_.on();
    dfplayer_.enqueue_init();
    dfplayer_.enqueue_volume(volume_);
    dfplayer_extend_lifetime();
  }

  void dfplayer_extend_lifetime() {
    shutdown_deadline_ = millis() + shutdown_delay_msec_;
  }

  bool dfplayer_powered_on() {
    return shutdown_deadline_ > 0;
  }

  bool dfplayer_should_power_off() {
    return dfplayer_powered_on() && shutdown_deadline_ <= millis() &&
      dfplayer_.work_pending() == 0;
  }

  void dfplayer_power_off() {
    dfplayer_.fini();
    mosfet_.off();
    shutdown_deadline_ = 0;
  }

  Net net_;
  DFPlayerAsync dfplayer_;
  Mosfet mosfet_;
  Firefly firefly_;
  Battery battery_;

  byte volume_;
  unsigned long shutdown_delay_msec_;
  unsigned long shutdown_deadline_;
  bool debug_enabled_;
};

// ------------------------------------------------------------------

const CricketConfig cricket_config = {
  .dfplayer_tx_pin = D10,
  .dfplayer_rx_pin = D9,
  .dfplayer_busy_pin = D8,
  .mosfet_pin = D5,              // matches GPIO_NUM_7
  .mosfet_gpio_num = GPIO_NUM_7, // matches D5
  .firefly_pin = D7,
  .battery_pin = A2,

  .shutdown_delay_msec = 10000,
  .initial_volume = 0x8, // 0x30 = max

  .ssid = "SSID",
  .pass = "PASSWORD",

  .port = 80,

  .debug_enabled = true,
};

Cricket cricket(cricket_config);

void setup() {
  Serial.begin(115200);
  cricket.setup();
}

// This doesn't actually preserve WiFi connections :(
void sleep(int msec) {
  cricket.sleep_enter();
  esp_sleep_enable_timer_wakeup(static_cast<uint64_t>(msec) * 1000);
  esp_sleep_enable_wifi_wakeup();
  esp_light_sleep_start();
  cricket.sleep_exit();
}

void loop() {
  cricket.loop();
  delay(1);
}
