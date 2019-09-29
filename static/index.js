new Vue({
    el: '#app',

    data: {
        ws: null, // Our websocket
        subjectId: null, // Id of the subject
        started: false,
        serverMessages: "",
        serverInstructions: "",
        pauseInstructions: ""
    },

    created: function() {
        var self = this;
        self.ws = new WebSocket('ws://' + window.location.host + '/ws');
        window.addEventListener('keydown', (e) => {
            if (self.started) {
                self.ws.send(
                    JSON.stringify({
                        subjectId: this.subjectId,
                        action: "KEY",
                        content: e.key,
                        keyCode: e.keyCode
                    })
                );
            }
        });
        this.ws.addEventListener('message', function(e) {
            var msg = JSON.parse(e.data);
            console.log(msg)
            switch (msg.kind) {
                case "INFO":
                    self.serverMessages += msg.message + '<br/>';
                    var element = document.getElementById('server-messages');
                    element.scrollTop = element.scrollHeight; // Auto scroll to the bottom    
                    break;
                case "INSTRUCTIONS":
                    self.serverInstructions = '<span class="card-title">Instructions</span>' + msg.message;
                    break;
                case "STUDY":
                    var element = document.getElementById('study');
                    element.text = '' + msg.message;
                    break;
                case "WAIT":
                    // Trigger the modal dialog here
                    self.pauseInstructions = '<span class="card-title">Instructions</span>' + msg.message;
                    var elems = document.querySelectorAll('.modal');
                    var instances = M.Modal.init(elems, {
                        onOpenStart: () => {
                            console.log("Opening!")
                        },
                        onCloseEnd: () => {
                            console.log("Closing!")
                            self.ws.send(
                                JSON.stringify({
                                    subjectId: this.subjectId,
                                    action: "CONTINUE"
                                })
                            );
                        }
                    });
                    instances[0].open()
                    break;
                case "BEGIN":
                    self.serverMessages += "Beginning for id = " + msg.message + '<br/>';
                    break;
                case "END":
                    self.serverMessages += "Ending for id = " + msg.message + '<br/>';
                    self.started = false;
                    break;
                case "CANCEL":
                    self.serverMessages += "Canceled for id = " + msg.message + '<br/>';
                    break;
            }
        });
    },

    methods: {
        start: function() {
            var self = this;
            if (!this.subjectId) {
                Materialize.toast('You must enter an id for the subject', 2000);
                return
            }
            this.subjectId = $('<p>').html(this.subjectId).text();
            this.ws.send(
                JSON.stringify({
                    subjectId: self.subjectId,
                    action: "START"
                })
            );
            self.started = true;
        },
        cancel: function() {
            var self = this;
            this.ws.send(
                JSON.stringify({
                    subjectId: self.subjectId,
                    action: "CANCEL"
                })
            );
        },
    }
});