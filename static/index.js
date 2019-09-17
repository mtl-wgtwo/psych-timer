new Vue({
  el: '#app',

  data: {
    ws: null, // Our websocket
    subjectId: null, // Id of the subject
    started: false,
    serverMessages: ""
  },

  created: function () {
    var self = this;
    this.ws = new WebSocket('ws://' + window.location.host + '/ws');
    window.addEventListener('keydown', (e) => {
      if (this.started) {
        this.ws.send(
          JSON.stringify({
            subjectId: this.subjectId,
            action: "KEY",
            content: e.key
          })
        );
      }
    });
    this.ws.addEventListener('message', function (e) {
      var msg = JSON.parse(e.data);
      console.log(msg)
      switch (msg.kind) {
        case "INFO":
          self.serverMessages += msg.message + '<br/>'; // Parse emojis

          var element = document.getElementById('server-messages');
          element.scrollTop = element.scrollHeight; // Auto scroll to the bottom
          break;
        case "BEGIN":
          break;
        case "END":
          this.started = false;
          break;
      }
    });
  },

  methods: {
    start: function () {
      if (!this.subjectId) {
        Materialize.toast('You must enter an id for the subject', 2000);
        return
      }
      this.subjectId = $('<p>').html(this.subjectId).text();
      this.ws.send(
        JSON.stringify({
          subjectId: this.subjectId,
          action: "START"
        })
      );
      this.started = true;
    },
  }
});